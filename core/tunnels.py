import re
import time
import asyncio
from dataclasses import dataclass
from typing import Dict, List
from core.config import logger


@dataclass
class TunnelInfo:
    port: int
    url: str
    process: asyncio.subprocess.Process
    created_at: float
    task: asyncio.Task


class CloudflareTunnelManager:
    """Administrador asíncrono no bloqueante de túneles Cloudflare (trycloudflare.com)."""

    def __init__(self):
        self.tunnels: Dict[int, TunnelInfo] = {}
        self._lock = asyncio.Lock()

    async def start_tunnel(self, port: int, timeout: float = 20.0) -> str:
        """Inicia un Quick Tunnel de Cloudflare para el puerto local y obtiene su URL pública."""
        async with self._lock:
            if port in self.tunnels:
                info = self.tunnels[port]
                if info.process.returncode is None:
                    return info.url
                else:
                    self._cleanup_tunnel(port)

            if not (1 <= port <= 65535):
                raise ValueError(f"Puerto inválido ({port}). Debe estar entre 1 y 65535.")

            cmd = [
                "cloudflared",
                "tunnel",
                "--url", f"http://localhost:{port}",
                "--no-autoupdate",
            ]

            process = await asyncio.create_subprocess_exec(
                *cmd,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.STDOUT,
            )

            url_future = asyncio.get_event_loop().create_future()
            url_pattern = re.compile(r"(https://[a-zA-Z0-9\.\-]+\.trycloudflare\.com)")

            async def monitor_output():
                try:
                    while True:
                        line_bytes = await process.stdout.readline()
                        if not line_bytes:
                            break
                        decoded = line_bytes.decode("utf-8", errors="replace").strip()
                        if not decoded:
                            continue

                        if not url_future.done():
                            match = url_pattern.search(decoded)
                            if match:
                                url_future.set_result(match.group(1))

                    if not url_future.done():
                        url_future.set_exception(
                            RuntimeError("Cloudflared finalizó sin devolver una URL de túnel.")
                        )
                except Exception as e:
                    if not url_future.done():
                        url_future.set_exception(e)
                finally:
                    if port in self.tunnels and self.tunnels[port].process == process:
                        self._cleanup_tunnel(port)

            task = asyncio.create_task(monitor_output())

            try:
                url = await asyncio.wait_for(url_future, timeout=timeout)
                self.tunnels[port] = TunnelInfo(
                    port=port,
                    url=url,
                    process=process,
                    created_at=time.time(),
                    task=task,
                )
                logger.info(f"Túnel Cloudflare iniciado con éxito en puerto {port}: {url}")
                return url
            except asyncio.TimeoutError:
                task.cancel()
                try:
                    process.kill()
                    await process.wait()
                except Exception:
                    pass
                raise TimeoutError(
                    f"Tiempo de espera agotado ({timeout}s) sin obtener respuesta de Cloudflare para el puerto {port}."
                )
            except Exception as e:
                task.cancel()
                try:
                    process.kill()
                    await process.wait()
                except Exception:
                    pass
                raise e

    def _cleanup_tunnel(self, port: int):
        info = self.tunnels.pop(port, None)
        if info:
            try:
                if info.process.returncode is None:
                    info.process.kill()
            except Exception:
                pass
            if not info.task.done():
                info.task.cancel()

    async def stop_tunnel(self, port: int) -> bool:
        """Detiene un túnel activo por puerto."""
        async with self._lock:
            if port not in self.tunnels:
                return False
            info = self.tunnels.pop(port)
            try:
                if info.process.returncode is None:
                    info.process.terminate()
                    try:
                        await asyncio.wait_for(info.process.wait(), timeout=2.0)
                    except asyncio.TimeoutError:
                        info.process.kill()
                        await info.process.wait()
            except Exception as e:
                logger.error(f"Error cerrando túnel Cloudflare en puerto {port}: {e}")
            if not info.task.done():
                info.task.cancel()
            logger.info(f"Túnel Cloudflare en puerto {port} cerrado correctamente.")
            return True

    async def stop_all_tunnels(self) -> int:
        """Detiene todos los túneles activos."""
        async with self._lock:
            ports = list(self.tunnels.keys())
            closed = 0
            for p in ports:
                info = self.tunnels.pop(p, None)
                if info:
                    try:
                        if info.process.returncode is None:
                            info.process.kill()
                            await info.process.wait()
                        closed += 1
                    except Exception:
                        pass
                    if not info.task.done():
                        info.task.cancel()
            return closed

    def list_tunnels(self) -> List[dict]:
        """Obtiene la lista de túneles vivos."""
        alive = []
        for port, info in list(self.tunnels.items()):
            if info.process.returncode is None:
                alive.append({
                    "port": port,
                    "url": info.url,
                    "uptime_seconds": int(time.time() - info.created_at),
                })
            else:
                self._cleanup_tunnel(port)
        return alive


tunnel_manager = CloudflareTunnelManager()
