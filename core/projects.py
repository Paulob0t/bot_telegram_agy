import os
import math
from typing import List, Tuple, Optional
from telegram import InlineKeyboardButton, InlineKeyboardMarkup
from core.config import HOME_DIR, PROJECTS_BASE_DIR
from core.state import session_state
from core.utils import get_git_branch


def list_available_projects() -> List[Tuple[str, str, float]]:
    """
    Lista las carpetas dentro de PROJECTS_BASE_DIR ordenadas por fecha de
    modificación reciente (más recientes primero).
    Retorna lista de tuplas: (nombre_carpeta, ruta_absoluta, mtime)
    """
    if not os.path.isdir(PROJECTS_BASE_DIR):
        return []

    entries = []
    try:
        for name in os.listdir(PROJECTS_BASE_DIR):
            if name.startswith("."):
                continue
            full_path = os.path.join(PROJECTS_BASE_DIR, name)
            if os.path.isdir(full_path):
                try:
                    mtime = os.path.getmtime(full_path)
                except Exception:
                    mtime = 0.0
                entries.append((name, full_path, mtime))
    except Exception:
        return []

    entries.sort(key=lambda x: x[2], reverse=True)
    return entries


def format_active_cwd_summary() -> str:
    """Devuelve un resumen formateado del directorio activo y su rama Git si aplica."""
    cwd = session_state.current_cwd
    short_name = session_state.get_cwd_short_name(cwd)
    branch = get_git_branch(cwd)
    branch_str = f" • 🌿 `{branch}`" if branch else ""
    return f"📂 **{short_name}**{branch_str}\n`{cwd}`"


def build_projects_keyboard(page: int = 0, page_size: int = 8) -> Tuple[InlineKeyboardMarkup, str]:
    """Construye el teclado inline paginado para seleccionar proyecto (/project o /cd)."""
    projects = list_available_projects()
    total_projects = len(projects)
    total_pages = max(1, math.ceil(total_projects / page_size))
    page = max(0, min(page, total_pages - 1))

    start_idx = page * page_size
    end_idx = start_idx + page_size
    current_slice = projects[start_idx:end_idx]

    active_cwd = os.path.abspath(session_state.current_cwd)
    is_home_active = active_cwd == os.path.abspath(HOME_DIR)

    keyboard: List[List[InlineKeyboardButton]] = []

    # Botón destacado para Home (~)
    home_prefix = "✅ " if is_home_active else "🏠 "
    keyboard.append([
        InlineKeyboardButton(f"{home_prefix}Home ({HOME_DIR})", callback_data="proj_home")
    ])

    # Botones de proyectos (2 por fila para comodidad en móvil)
    row: List[InlineKeyboardButton] = []
    for name, full_path, _ in current_slice:
        is_selected = os.path.abspath(full_path) == active_cwd
        prefix = "✅ " if is_selected else "📁 "
        label_name = name[:20] + "…" if len(name) > 21 else name
        btn_text = f"{prefix}{label_name}"

        # Asegurar límite de 64 bytes en callback_data
        cb_payload = f"proj_sel:{name}"
        if len(cb_payload.encode("utf-8")) > 64:
            # Fallback por índice absoluto si el nombre es muy largo
            abs_idx = projects.index((name, full_path, _))
            cb_payload = f"proj_idx:{abs_idx}"

        row.append(InlineKeyboardButton(btn_text, callback_data=cb_payload))
        if len(row) == 2:
            keyboard.append(row)
            row = []

    if row:
        keyboard.append(row)

    # Fila de paginación
    if total_pages > 1:
        nav_row = []
        if page > 0:
            nav_row.append(InlineKeyboardButton("◀️ Anterior", callback_data=f"proj_page:{page - 1}"))
        nav_row.append(InlineKeyboardButton(f"📄 {page + 1}/{total_pages}", callback_data="res_noop"))
        if page < total_pages - 1:
            nav_row.append(InlineKeyboardButton("Siguiente ▶️", callback_data=f"proj_page:{page + 1}"))
        keyboard.append(nav_row)

    active_summary = format_active_cwd_summary()
    text = (
        "🗂️ **Selector de Proyecto / Directorio de Trabajo (`cwd`)**\n\n"
        f"📍 **Activo actualmente:**\n{active_summary}\n\n"
        f"📦 **Proyectos en `{PROJECTS_BASE_DIR}` ({total_projects}):**\n"
        f"Página **{page + 1} de {total_pages}** • *Toca un proyecto para activar su workspace o usa `/cd <nombre>`:*"
    )
    return InlineKeyboardMarkup(keyboard), text


def resolve_project_Target(query_str: str) -> Tuple[Optional[str], List[Tuple[str, str]]]:
    """
    Resuelve un argumento pasado a `/cd <arg>` o `/project <arg>`.
    Retorna:
      - (ruta_exacta, []) si encontró un único directorio válido.
      - (None, lista_coincidencias) si hay múltiples coincidencias parciales o ninguna.
    """
    q = query_str.strip()
    if not q or q in ("~", "home", "$HOME"):
        return HOME_DIR, []

    # Si es una ruta absoluta o empieza con ~ / .
    if q.startswith("/") or q.startswith("~"):
        expanded = os.path.abspath(os.path.expanduser(q))
        if os.path.isdir(expanded):
            return expanded, []

    # Buscar dentro de PROJECTS_BASE_DIR
    projects = list_available_projects()
    q_lower = q.lower()

    # 1. Coincidencia exacta por nombre (case-insensitive)
    for name, full_path, _ in projects:
        if name.lower() == q_lower:
            return full_path, []

    # 2. Coincidencia parcial (substring)
    matches = [
        (name, full_path)
        for name, full_path, _ in projects
        if q_lower in name.lower()
    ]
    if len(matches) == 1:
        return matches[0][1], []

    return None, matches
