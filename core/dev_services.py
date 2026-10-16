import os
import re
import time
import signal
import socket
import asyncio
import subprocess
from collections import deque
from dataclasses import dataclass, field
from typing import Dict, Optional, Deque, List, Tuple
from telegram import InlineKeyboardButton, InlineKeyboardMarkup
from core.config import PROJECTS_BASE_DIR, logger
from core.utils import format_uptime
from core.tunnels import tunnel_manager

WICODE_BACK_DIR = os.path.join(PROJECTS_BASE_DIR, "wicode-backend")
WICODE_FRONT_DIR = os.path.join(PROJECTS_BASE_DIR, "wicode-frontend")
BOT_LOG_PATH = os.path.expanduser("~/.local/state/telegram-bot.log")

ANSI_RE = re.compile(r"\x1B(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~])")


def is_port_listening(port: int, host: str = "127.0.0.1") -> bool:
    """Verifica si un puerto TCP local está escuchando conexiones."""
    for h in (host, "::1", "0.0.0.0"):
        try:
            with socket.create_connection((h, port), timeout=0.35):
                return True
        except Exception:
            continue
    return False


def kill_processes_on_port(port: int) -> bool:
    """Termina cualquier proceso que esté escuchando en el puerto indicado (incluso si fue iniciado fuera del bot)."""
    if not is_port_listening(port):
        return False
    try:
        subprocess.run(
            ["fuser", "-k", "-TERM", f"{port}/tcp"],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            timeout=2.0,
        )
        time.sleep(0.5)
        if is_port_listening(port):
            subprocess.run(
                ["fuser", "-k", "-KILL", f"{port}/tcp"],
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
                timeout=2.0,
            )
        return True
    except Exception:
        return False


@dataclass
class ManagedService:
    key: str
    name: str
    icon: str
    cwd: str
    cmd: List[str]
    port: int
    process: Optional[asyncio.subprocess.Process] = None
    started_at: Optional[float] = None
    reader_task: Optional[asyncio.Task] = None
    logs: Deque[str] = field(default_factory=lambda: deque(maxlen=120))


class DevServiceManager:
    """
    Gestor asíncrono de servidores locales de desarrollo (WiCode Backend,
    WiCode Frontend y Cloud SQL Proxy) con captura de logs y limpieza de puertos.
    """

    def __init__(self):
        self._lock = asyncio.Lock()
        self.services: Dict[str, ManagedService] = {
            "proxy": ManagedService(
                key="proxy",
                name="Cloud SQL Proxy",
                icon="🗄️",
                cwd=WICODE_BACK_DIR,
                cmd=["pnpm", "run", "proxy"],
                port=5433,
            ),
            "backend": ManagedService(
                key="backend",
                name="WiCode Backend",
                icon="⚙️",
                cwd=WICODE_BACK_DIR,
                cmd=["pnpm", "run", "dev"],
                port=3000,
            ),
            "frontend": ManagedService(
                key="frontend",
                name="WiCode Frontend",
                icon="🎨",
                cwd=WICODE_FRONT_DIR,
                cmd=["pnpm", "run", "dev"],
                port=5173,
            ),
        }

    def is_service_running(self, key: str) -> Tuple[bool, str]:
        """
        Retorna (esta_activo, detalle_origen).
        Detecta tanto subprocesos administrados por el bot como procesos externos en el puerto.
        """
        svc = self.services.get(key)
        if not svc:
            return False, "No registrado"

        proc_alive = svc.process is not None and svc.process.returncode is None
        port_open = is_port_listening(svc.port)

        if proc_alive and port_open:
            up = format_uptime(int(time.time() - (svc.started_at or time.time())))
            return True, f"🟢 Activo (`:{svc.port}` • `{up}`)"
        if proc_alive and not port_open:
            up = format_uptime(int(time.time() - (svc.started_at or time.time())))
            return True, f"🟡 Iniciando (`:{svc.port}` • `{up}`)"
        if port_open:
            return True, f"🟢 Activo externo (`:{svc.port}`)"
        return False, f"⚪ Apagado (`:{svc.port}`)"

    async def start_service(self, key: str, wait_port_seconds: float = 4.0) -> bool:
        """Inicia un servicio de desarrollo si no está ya corriendo."""
        async with self._lock:
            svc = self.services.get(key)
            if not svc:
                raise ValueError(f"Servicio desconocido: {key}")

            if not os.path.isdir(svc.cwd):
                raise FileNotFoundError(f"No existe el directorio `{svc.cwd}`")

            # Si ya está escuchando el puerto o el proceso sigue vivo, no duplicar
            if is_port_listening(svc.port) or (svc.process and svc.process.returncode is None):
                return True

            env = os.environ.copy()
            env["PATH"] = (
                f"/home/paulobot/.local/bin:/home/paulobot/.nvm/versions/node:"
                f"/usr/local/bin:/usr/bin:/bin:{env.get('PATH', '')}"
            )

            svc.logs.clear()
            svc.logs.append(f"[{time.strftime('%H:%M:%S')}] Iniciando {' '.join(svc.cmd)} en {svc.cwd}...")

            proc = await asyncio.create_subprocess_exec(
                *svc.cmd,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.STDOUT,
                cwd=svc.cwd,
                env=env,
                start_new_session=True,
            )
            svc.process = proc
            svc.started_at = time.time()

            async def _read_logs():
                try:
                    while True:
                        line_bytes = await proc.stdout.readline()
                        if not line_bytes:
                            break
                        raw = line_bytes.decode("utf-8", errors="replace").rstrip()
                        clean = ANSI_RE.sub("", raw).strip()
                        if clean:
                            svc.logs.append(f"[{time.strftime('%H:%M:%S')}] {clean}")
                except Exception:
                    pass

            svc.reader_task = asyncio.create_task(_read_logs())

        # Esperar brevemente a que el puerto levante
        deadline = time.time() + wait_port_seconds
        while time.time() < deadline:
            if is_port_listening(svc.port):
                return True
            if svc.process and svc.process.returncode is not None:
                return False
            await asyncio.sleep(0.4)

        return svc.process is not None and svc.process.returncode is None

    async def stop_service(self, key: str) -> bool:
        """Detiene el proceso del servicio y libera el puerto TCP."""
        async with self._lock:
            svc = self.services.get(key)
            if not svc:
                return False

            proc = svc.process
            killed_any = False

            if proc and proc.returncode is None:
                killed_any = True
                pid = proc.pid
                try:
                    pgid = os.getpgid(pid)
                    os.killpg(pgid, signal.SIGTERM)
                except Exception:
                    try:
                        proc.terminate()
                    except Exception:
                        pass

                try:
                    await asyncio.wait_for(proc.wait(), timeout=2.0)
                except asyncio.TimeoutError:
                    try:
                        pgid = os.getpgid(pid)
                        os.killpg(pgid, signal.SIGKILL)
                    except Exception:
                        try:
                            proc.kill()
                        except Exception:
                            pass

            svc.process = None
            svc.started_at = None
            if svc.reader_task and not svc.reader_task.done():
                svc.reader_task.cancel()

            # Asegurar que ningún proceso externo quede ocupando el puerto
            if is_port_listening(svc.port):
                if kill_processes_on_port(svc.port):
                    killed_any = True

            svc.logs.append(f"[{time.strftime('%H:%M:%S')}] Servicio detenido.")
            return killed_any

    async def start_wicode_fullstack(self) -> Dict[str, bool]:
        """Levanta Proxy (:5433), Backend (:3000) y Frontend (:5173) en orden."""
        results = {}
        results["proxy"] = await self.start_service("proxy", wait_port_seconds=2.5)
        results["backend"] = await self.start_service("backend", wait_port_seconds=4.0)
        results["frontend"] = await self.start_service("frontend", wait_port_seconds=3.5)
        return results

    async def stop_wicode_fullstack(self) -> Dict[str, bool]:
        """Detiene Frontend, Backend y Proxy limpiamente."""
        results = {}
        results["frontend"] = await self.stop_service("frontend")
        results["backend"] = await self.stop_service("backend")
        results["proxy"] = await self.stop_service("proxy")
        return results

    def get_service_logs(self, key: str, max_lines: int = 25) -> str:
        svc = self.services.get(key)
        if not svc or not svc.logs:
            return f"ℹ️ No hay logs registrados en memoria para **{svc.name if svc else key}**."
        recent = list(svc.logs)[-max_lines:]
        body = "\n".join(recent)[-3400:]
        return f"📋 **Últimos logs de {svc.icon} {svc.name} (`:{svc.port}`):**\n\n```\n{body}\n```"

    def get_recent_errors_report(self) -> str:
        """Recopila líneas de error recientes de los servicios WiCode y del log del bot."""
        err_keywords = ("error", "exception", "fail", "fatal", "err!", "eaddrinuse", "prisma", "syntaxerror")
        sections: List[str] = []

        for key, svc in self.services.items():
            matched = [
                line for line in svc.logs
                if any(k in line.lower() for k in err_keywords)
            ]
            if matched:
                snippet = "\n".join(matched[-10:])[-1000:]
                sections.append(f"{svc.icon} **{svc.name} (`:{svc.port}`):**\n```\n{snippet}\n```")

        # Revisar también las últimas líneas de error del propio bot
        if os.path.exists(BOT_LOG_PATH):
            try:
                with open(BOT_LOG_PATH, "r", encoding="utf-8", errors="replace") as f:
                    tail_lines = deque(f, maxlen=80)
                bot_errs = [
                    line.strip()
                    for line in tail_lines
                    if any(k in line.lower() for k in ("error", "exception", "traceback"))
                ]
                if bot_errs:
                    snippet = "\n".join(bot_errs[-8:])[-900:]
                    sections.append(f"🤖 **Telegram Bot (`telegram-bot.log`):**\n```\n{snippet}\n```")
            except Exception:
                pass

        if not sections:
            return "✅ **Sin errores recientes:** No se detectaron errores en Backend, Frontend, Proxy ni en el Bot."

        return "⚠️ **Reporte de Errores Recientes (`/dev`):**\n\n" + "\n\n".join(sections)


dev_manager = DevServiceManager()


def build_dev_dashboard() -> Tuple[InlineKeyboardMarkup, str]:
    """Construye el panel interactivo y teclado inline para el comando /dev."""
    _, proxy_status = dev_manager.is_service_running("proxy")
    back_running, back_status = dev_manager.is_service_running("backend")
    front_running, front_status = dev_manager.is_service_running("frontend")

    active_tunnels = {t["port"]: t["url"] for t in tunnel_manager.list_tunnels()}
    tun_3000 = active_tunnels.get(3000)
    tun_5173 = active_tunnels.get(5173)

    lines = [
        "🛠️ **Panel de Servicios de Desarrollo (`/dev`)**\n",
        f"• 🗄️ **Cloud SQL Proxy:** {proxy_status}",
        f"• ⚙️ **WiCode Backend:** {back_status}",
        f"• 🎨 **WiCode Frontend:** {front_status}\n",
        "☁️ **Túneles Cloudflare WiCode:**",
        f"• 🎨 **Frontend (`:5173`):** `{tun_5173 if tun_5173 else 'Sin túnel'}`",
        f"• ⚙️ **Backend (`:3000`):** `{tun_3000 if tun_3000 else 'Sin túnel'}`",
    ]

    keyboard: List[List[InlineKeyboardButton]] = [
        [
            InlineKeyboardButton("🚀 Iniciar WiCode Fullstack", callback_data="dev:start_all"),
            InlineKeyboardButton("🛑 Detener WiCode", callback_data="dev:stop_all"),
        ],
        [
            InlineKeyboardButton(
                f"{'🛑 Parar' if back_running else '▶️ Iniciar'} Backend (:3000)",
                callback_data="dev:toggle:backend",
            ),
            InlineKeyboardButton(
                f"{'🛑 Parar' if front_running else '▶️ Iniciar'} Frontend (:5173)",
                callback_data="dev:toggle:frontend",
            ),
        ],
        [
            InlineKeyboardButton("☁️ Abrir Túneles (5173 + 3000)", callback_data="dev:tunnels_both"),
        ],
        [
            InlineKeyboardButton("📋 Logs Back", callback_data="dev:logs:backend"),
            InlineKeyboardButton("📋 Logs Front", callback_data="dev:logs:frontend"),
            InlineKeyboardButton("⚠️ Errores", callback_data="dev:errors"),
        ],
        [
            InlineKeyboardButton("🔄 Actualizar Estado", callback_data="dev:refresh"),
        ],
    ]

    return InlineKeyboardMarkup(keyboard), "\n".join(lines)
