import os
import asyncio
from typing import List, Tuple
from telegram import InlineKeyboardButton, InlineKeyboardMarkup
from core.state import session_state
from core.utils import get_git_branch


async def run_git_cmd(args: List[str], cwd: str = "", timeout: float = 15.0) -> Tuple[int, str]:
    """Ejecuta un subcomando de git de forma asíncrona en el directorio activo."""
    target_cwd = cwd or session_state.current_cwd
    if not os.path.isdir(target_cwd):
        return 1, f"El directorio `{target_cwd}` no existe."

    proc = await asyncio.create_subprocess_exec(
        "git",
        *args,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.STDOUT,
        cwd=target_cwd,
    )
    try:
        stdout_bytes, _ = await asyncio.wait_for(proc.communicate(), timeout=timeout)
    except asyncio.TimeoutError:
        try:
            proc.kill()
            await proc.wait()
        except Exception:
            pass
        return 1, f"Tiempo agotado ({timeout}s) ejecutando `git {' '.join(args)}`."

    output = stdout_bytes.decode("utf-8", errors="replace").strip()
    return proc.returncode or 0, output


def is_git_repo(path: str) -> bool:
    return bool(path and os.path.isdir(path) and os.path.exists(os.path.join(path, ".git")))


def build_git_keyboard() -> InlineKeyboardMarkup:
    keyboard = [
        [
            InlineKeyboardButton("📊 Status (-sb)", callback_data="git:status"),
            InlineKeyboardButton("📈 Diff (--stat)", callback_data="git:diff"),
        ],
        [
            InlineKeyboardButton("📜 Log (Últimos 5)", callback_data="git:log"),
            InlineKeyboardButton("🌿 Ramas", callback_data="git:branches"),
        ],
        [
            InlineKeyboardButton("⬇️ Git Pull", callback_data="git:pull"),
            InlineKeyboardButton("⬆️ Git Push...", callback_data="git:push_ask"),
        ],
        [
            InlineKeyboardButton("🗂️ Cambiar Proyecto (/cd)", callback_data="proj_page:0"),
        ],
    ]
    return InlineKeyboardMarkup(keyboard)


async def build_git_dashboard(custom_title: str = "", cmd_output: str = "") -> Tuple[InlineKeyboardMarkup, str]:
    """Construye el panel interactivo de Git para el proyecto activo."""
    cwd = session_state.current_cwd
    short_name = session_state.get_cwd_short_name(cwd)

    if not is_git_repo(cwd):
        kb = InlineKeyboardMarkup([
            [InlineKeyboardButton("🗂️ Elegir un repositorio en /project", callback_data="proj_page:0")]
        ])
        msg = (
            f"🐙 **Panel Rápido de Git (`/git`)**\n\n"
            f"⚠️ El directorio activo (`{short_name}`) no es un repositorio Git:\n"
            f"`{cwd}`\n\n"
            "*Selecciona primero un proyecto con `/cd` o toca el botón inferior:*"
        )
        return kb, msg

    branch = get_git_branch(cwd) or "HEAD"

    if not cmd_output:
        _, status_out = await run_git_cmd(["status", "-sb"], cwd=cwd)
        cmd_output = status_out or "Árbol de trabajo limpio (sin cambios)."
        custom_title = "📊 Estado del Repositorio (`git status -sb`)"

    clean_out = cmd_output[:3200]
    text = (
        f"🐙 **Git Rápido — 📂 `{short_name}`** (🌿 `{branch}`)\n"
        f"`{cwd}`\n\n"
        f"**{custom_title}:**\n"
        f"```\n{clean_out}\n```\n"
        "💡 *Tip: También puedes usar `/git commit <mensaje>` para hacer stage + commit al instante.*"
    )
    return build_git_keyboard(), text
