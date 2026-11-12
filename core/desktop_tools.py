import os
import glob
import tempfile
import asyncio
from typing import Tuple
from telegram import InlineKeyboardButton, InlineKeyboardMarkup
from core.config import IS_WINDOWS


def _get_wayland_env() -> dict:
    """Construye el entorno con XDG_RUNTIME_DIR y WAYLAND_DISPLAY para ejecutar grim desde systemd."""
    env = os.environ.copy()
    uid = os.getuid() if hasattr(os, "getuid") else 1000
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


async def _capture_windows_screenshot(tmp_png: str) -> Tuple[bool, str]:
    escaped_path = tmp_png.replace("'", "''")
    ps_script = (
        "Add-Type -AssemblyName System.Windows.Forms,System.Drawing; "
        "$bounds = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds; "
        "$bmp = New-Object System.Drawing.Bitmap $bounds.Width, $bounds.Height; "
        "$graphics = [System.Drawing.Graphics]::FromImage($bmp); "
        "$graphics.CopyFromScreen($bounds.Location, [System.Drawing.Point]::Empty, $bounds.Size); "
        f"$bmp.Save('{escaped_path}', [System.Drawing.Imaging.ImageFormat]::Png); "
        "$graphics.Dispose(); $bmp.Dispose()"
    )
    proc = await asyncio.create_subprocess_exec(
        "powershell",
        "-NoProfile",
        "-NonInteractive",
        "-Command",
        ps_script,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.STDOUT,
    )
    stdout_bytes, _ = await asyncio.wait_for(proc.communicate(), timeout=12.0)
    if proc.returncode == 0 and os.path.exists(tmp_png) and os.path.getsize(tmp_png) > 0:
        return True, tmp_png
    err_out = stdout_bytes.decode("utf-8", errors="replace").strip()
    if os.path.exists(tmp_png):
        os.remove(tmp_png)
    return False, err_out or f"PowerShell terminó con código {proc.returncode}"


async def capture_wayland_screenshot() -> Tuple[bool, str]:
    """
    Toma una captura de pantalla del escritorio (PowerShell en Windows, grim en Linux/Wayland).
    Retorna (exito, ruta_archivo_png_o_mensaje_error).
    """
    fd, tmp_png = tempfile.mkstemp(prefix="bot_screen_", suffix=".png")
    os.close(fd)

    try:
        if IS_WINDOWS:
            return await _capture_windows_screenshot(tmp_png)

        env = _get_wayland_env()
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
