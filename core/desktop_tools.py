import os
import glob
import time
import tempfile
import asyncio
from typing import Optional, Tuple
from telegram import InlineKeyboardButton, InlineKeyboardMarkup


def _get_wayland_env() -> dict:
    """Construye el entorno con XDG_RUNTIME_DIR y WAYLAND_DISPLAY para ejecutar grim/hyprctl desde systemd."""
    env = os.environ.copy()
    uid = os.getuid()
    runtime_dir = env.get("XDG_RUNTIME_DIR") or f"/run/user/{uid}"
    env["XDG_RUNTIME_DIR"] = runtime_dir

    if not env.get("WAYLAND_DISPLAY"):
        sockets = sorted(glob.glob(os.path.join(runtime_dir, "wayland-*")))
        sockets = [s for s in sockets if not s.endswith(".lock")]
        if sockets:
            env["WAYLAND_DISPLAY"] = os.path.basename(sockets[0])
        else:
            env["WAYLAND_DISPLAY"] = "wayland-1"

    return env


async def capture_wayland_screenshot() -> Tuple[bool, str]:
    """
    Toma una captura de pantalla del escritorio Wayland/Hyprland usando `grim`.
    Retorna (exito, ruta_archivo_png_o_mensaje_error).
    """
    env = _get_wayland_env()
    fd, tmp_png = tempfile.mkstemp(prefix="paulobot_screen_", suffix=".png")
    os.close(fd)

    try:
        proc = await asyncio.create_subprocess_exec(
            "grim",
            tmp_png,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.STDOUT,
            env=env,
        )
        stdout_bytes, _ = await asyncio.wait_for(proc.communicate(), timeout=8.0)
        if proc.returncode == 0 and os.path.exists(tmp_png) and os.path.getsize(tmp_png) > 0:
            return True, tmp_png
        err_out = stdout_bytes.decode("utf-8", errors="replace").strip()
        if os.path.exists(tmp_png):
            os.remove(tmp_png)
        return False, err_out or f"grim terminó con código {proc.returncode}"
    except Exception as e:
        if os.path.exists(tmp_png):
            try:
                os.remove(tmp_png)
            except Exception:
                pass
        return False, str(e)


def build_screenshot_keyboard() -> InlineKeyboardMarkup:
    return InlineKeyboardMarkup([
        [
            InlineKeyboardButton("🔄 Nueva Captura", callback_data="screen:capture"),
            InlineKeyboardButton("🗑️ Borrar Captura", callback_data="screen:delete"),
        ]
    ])
