"""
Descubrimiento y selector rápido de sesiones de Claude Code por proyecto (~/.claude/projects/*).
Optimizado con lectura head/tail (`seek`) para archivos .jsonl pesados (15MB+),
detección de sesiones en ejecución en terminal y borrado limpio.
"""
import glob
import json
import math
import os
import re
import subprocess
from typing import Dict, List, Optional, Tuple
from telegram import InlineKeyboardButton, InlineKeyboardMarkup
from core.config import CLAUDE_PROJECTS_DIR, HOME_DIR
from core.state import session_state

CHUNK_BYTES = 128 * 1024  # 128 KB head + 128 KB tail para parseo instantáneo
_META_CACHE: Dict[str, Tuple[float, Dict[str, str]]] = {}


def _clean_user_text(raw_content) -> str:
    if isinstance(raw_content, list):
        parts = [
            item.get("text", "")
            for item in raw_content
            if isinstance(item, dict) and item.get("type") == "text"
        ]
        raw_content = "\n".join(parts)
    if not isinstance(raw_content, str):
        return ""
    text = re.sub(
        r"<(system-reminder|local-command-[a-z-]+|command-[a-z-]+)>.*?</\1>",
        "",
        raw_content,
        flags=re.DOTALL,
    )
    text = re.sub(r"</?[a-zA-Z0-9_\-]+(?:\s+[^>]*)?>", "", text)
    for line in text.splitlines():
        clean = " ".join(line.strip(" #*>-•\t").split())
        if clean and not clean.startswith("/"):
            return clean
    return ""


def _parse_jsonl_lines(lines: List[str]):
    cwd = ""
    first_user = ""
    last_user = ""
    custom_title = ""

    for raw in lines:
        line = raw.strip()
        if not line or not line.startswith("{"):
            continue
        try:
            data = json.loads(line)
        except Exception:
            continue

        if not cwd and data.get("cwd"):
            cwd = data["cwd"]
        t = data.get("type")
        if t == "custom-title" and data.get("customTitle"):
            custom_title = str(data["customTitle"]).strip()
        elif t == "user" and not data.get("isMeta"):
            msg = data.get("message", {})
            one_line = _clean_user_text(msg.get("content", ""))
            if one_line:
                if not first_user:
                    first_user = one_line
                last_user = one_line
    return cwd, custom_title, first_user, last_user


def _extract_session_meta(jsonl_path: str) -> Optional[Dict[str, str]]:
    """
    Extrae session_id, cwd, title y mtime de un .jsonl de Claude Code en O(1)
    con caché en memoria por mtime y lectura head/tail de 128 KB.
    """
    if not os.path.isfile(jsonl_path):
        _META_CACHE.pop(jsonl_path, None)
        return None
    session_id = os.path.splitext(os.path.basename(jsonl_path))[0]

    try:
        st = os.stat(jsonl_path)
        mtime = st.st_mtime
        size = st.st_size
        if size == 0:
            return None

        cached = _META_CACHE.get(jsonl_path)
        if cached and cached[0] == mtime:
            return dict(cached[1])

        with open(jsonl_path, "rb") as f:
            if size <= CHUNK_BYTES * 2:
                raw_bytes = f.read()
                lines = raw_bytes.decode("utf-8", errors="replace").splitlines()
                cwd, custom_title, first_user, last_user = _parse_jsonl_lines(lines)
            else:
                head_bytes = f.read(CHUNK_BYTES)
                f.seek(max(0, size - CHUNK_BYTES))
                tail_bytes = f.read(CHUNK_BYTES)
                head_lines = head_bytes.decode("utf-8", errors="replace").splitlines()[:-1]
                tail_lines = tail_bytes.decode("utf-8", errors="replace").splitlines()[1:]
                cwd_h, title_h, first_h, last_h = _parse_jsonl_lines(head_lines)
                cwd_t, title_t, first_t, last_t = _parse_jsonl_lines(tail_lines)
                cwd = cwd_h or cwd_t
                custom_title = title_t or title_h
                first_user = first_h or first_t
                last_user = last_t or last_h or first_user
    except Exception:
        return None

    title = custom_title or last_user or first_user
    if not title:
        return None

    meta = {
        "session_id": session_id,
        "cwd": cwd or HOME_DIR,
        "title": title[:60],
        "mtime": str(mtime),
        "path": jsonl_path,
    }
    _META_CACHE[jsonl_path] = (mtime, dict(meta))
    return meta


def is_claude_busy_in_cwd(cwd: str, session_id: Optional[str] = None) -> bool:
    """
    Detecta si hay un proceso interactivo de `claude` abierto en la terminal
    sobre el mismo proyecto o sesión para evitar colisiones de `.jsonl`.
    """
    if not cwd:
        return False
    target_cwd = os.path.abspath(cwd)
    try:
        for pid_str in os.listdir("/proc"):
            if not pid_str.isdigit():
                continue
            try:
                cmdline_path = f"/proc/{pid_str}/cmdline"
                with open(cmdline_path, "rb") as f:
                    raw = f.read().decode("utf-8", errors="ignore")
                args = [a for a in raw.split("\x00") if a]
                if not args:
                    continue
                bin_name = os.path.basename(args[0])
                if bin_name != "claude" and not any(a.endswith("/claude") for a in args[:2]):
                    continue
                # Ignorar procesos no interactivos (-p) lanzados por el propio bot o ai-token-stats
                if "-p" in args or "--print" in args:
                    continue
                if session_id and session_id in args:
                    return True
                proc_cwd = os.path.abspath(os.readlink(f"/proc/{pid_str}/cwd"))
                if proc_cwd == target_cwd:
                    return True
            except Exception:
                continue
    except Exception:
        pass
    return False


def list_claude_projects() -> List[Dict[str, object]]:
    """
    Retorna los proyectos que tienen sesiones reales en ~/.claude/projects/,
    ordenados por fecha de modificación más reciente (priorizando proyectos fuera de ~ y /tmp).
    """
    if not os.path.isdir(CLAUDE_PROJECTS_DIR):
        return []

    projects: List[Dict[str, object]] = []
    for slug in os.listdir(CLAUDE_PROJECTS_DIR):
        if slug in ("-tmp", "-home-paulobot"):
            continue
        proj_dir = os.path.join(CLAUDE_PROJECTS_DIR, slug)
        if not os.path.isdir(proj_dir):
            continue

        sessions = list_claude_sessions_for_slug(slug)
        if not sessions:
            continue

        cwd = sessions[0]["cwd"]
        short_name = os.path.basename(cwd.rstrip("/")) or slug
        latest_mtime = max(float(s["mtime"]) for s in sessions)
        projects.append({
            "slug": slug,
            "name": short_name,
            "cwd": cwd,
            "count": len(sessions),
            "mtime": latest_mtime,
        })

    projects.sort(key=lambda x: float(x["mtime"]), reverse=True)
    return projects


def get_claude_slug_for_cwd(cwd: str) -> Optional[str]:
    """Devuelve el slug en ~/.claude/projects correspondiente a un directorio `cwd`, si tiene chats."""
    target = os.path.abspath(cwd)
    for proj in list_claude_projects():
        if os.path.abspath(str(proj["cwd"])) == target:
            return str(proj["slug"])
    slug_guess = "-" + target.strip("/").replace("/", "-")
    if os.path.isdir(os.path.join(CLAUDE_PROJECTS_DIR, slug_guess)):
        return slug_guess
    return None


def list_claude_sessions_for_cwd(cwd: str) -> List[Dict[str, str]]:
    """Lista las sesiones de Claude para el `cwd` actual (o todas las de proyectos si no hay en cwd)."""
    slug = get_claude_slug_for_cwd(cwd)
    if slug:
        sessions = list_claude_sessions_for_slug(slug)
        if sessions:
            return sessions
    all_sessions: List[Dict[str, str]] = []
    for proj in list_claude_projects():
        all_sessions.extend(list_claude_sessions_for_slug(str(proj["slug"])))
    all_sessions.sort(key=lambda x: float(x["mtime"]), reverse=True)
    return all_sessions


def list_claude_sessions_for_slug(slug: str) -> List[Dict[str, str]]:
    """Lista las sesiones (.jsonl) de un proyecto de Claude ordenadas por más reciente."""
    proj_dir = os.path.join(CLAUDE_PROJECTS_DIR, slug)
    if not os.path.isdir(proj_dir):
        return []

    items: List[Dict[str, str]] = []
    for path in glob.glob(os.path.join(proj_dir, "*.jsonl")):
        meta = _extract_session_meta(path)
        if meta:
            meta["slug"] = slug
            items.append(meta)

    items.sort(key=lambda x: float(x["mtime"]), reverse=True)
    return items


def find_session_by_prefix(slug: str, prefix: str) -> Optional[Dict[str, str]]:
    for s in list_claude_sessions_for_slug(slug):
        if s["session_id"].startswith(prefix):
            return s
    return None


def delete_claude_session(slug: str, session_id: str) -> bool:
    """Elimina un archivo .jsonl de sesión de Claude Code y su subdirectorio si existe."""
    import shutil
    proj_dir = os.path.join(CLAUDE_PROJECTS_DIR, slug)
    path = os.path.join(proj_dir, f"{session_id}.jsonl")
    sub_dir = os.path.join(proj_dir, session_id)
    removed = False
    if os.path.isfile(path):
        try:
            os.remove(path)
            _META_CACHE.pop(path, None)
            removed = True
        except Exception:
            return False
    if os.path.isdir(sub_dir):
        shutil.rmtree(sub_dir, ignore_errors=True)
    if removed and session_state.claude_session_id == session_id:
        session_state.claude_session_id = None
        session_state.claude_session_title = "🔄 Último chat de Claude (-c)"
        session_state.save_state()
    return removed


def build_claude_delete_keyboard(proj_idx: int = 0, page: int = 0, page_size: int = 5) -> Tuple[Optional[InlineKeyboardMarkup], str]:
    projects = list_claude_projects()
    if not projects:
        return None, "ℹ️ No hay sesiones de Claude Code guardadas para eliminar."
    proj_idx = max(0, min(proj_idx, len(projects) - 1))
    proj = projects[proj_idx]
    slug = str(proj["slug"])
    sessions = list_claude_sessions_for_slug(slug)
    total = len(sessions)
    if total == 0:
        return None, f"ℹ️ No hay chats de Claude para eliminar en `{proj['name']}`."

    total_pages = max(1, math.ceil(total / page_size))
    page = max(0, min(page, total_pages - 1))
    page_items = sessions[page * page_size : (page + 1) * page_size]

    keyboard = []
    for s in page_items:
        sid = s["session_id"]
        short_id = sid[:8]
        title = s["title"]
        btn_label = f"🗑️ {title[:30]}..." if len(title) > 30 else f"🗑️ {title}"
        keyboard.append([
            InlineKeyboardButton(btn_label, callback_data=f"cl_del_ask:{proj_idx}:{short_id}")
        ])

    if total_pages > 1:
        nav = []
        if page > 0:
            nav.append(InlineKeyboardButton("◀️ Anterior", callback_data=f"cl_del_page:{proj_idx}:{page - 1}"))
        nav.append(InlineKeyboardButton(f"📄 {page + 1}/{total_pages}", callback_data="res_noop"))
        if page < total_pages - 1:
            nav.append(InlineKeyboardButton("Siguiente ▶️", callback_data=f"cl_del_page:{proj_idx}:{page + 1}"))
        keyboard.append(nav)

    keyboard.append([
        InlineKeyboardButton(f"🔙 Volver a {proj['name']}", callback_data=f"cl_proj:{proj_idx}:0"),
        InlineKeyboardButton("🔵 Borrar de AGY", callback_data="del_menu:0"),
    ])

    text = (
        f"🗑️ **Eliminar Chats de Claude en `{proj['name']}`**\n"
        f"Página **{page + 1} de {total_pages}** • *Toca una sesión para borrar su `.jsonl`:*"
    )
    return InlineKeyboardMarkup(keyboard), text


def build_claude_projects_keyboard() -> Tuple[InlineKeyboardMarkup, str]:
    projects = list_claude_projects()
    engine_badge = "🟠 **ACTIVO (Claude Code)**" if session_state.current_engine == "claude" else "🔵 **Inactivo (Usando AGY)**"

    keyboard = []
    for idx, p in enumerate(projects):
        is_current = (
            session_state.current_engine == "claude"
            and os.path.abspath(session_state.current_cwd) == os.path.abspath(str(p["cwd"]))
        )
        prefix = "✅ " if is_current else "🟠 "
        keyboard.append([
            InlineKeyboardButton(
                f"{prefix}{p['name']} ({p['count']} chats)",
                callback_data=f"cl_proj:{idx}:0",
            )
        ])

    if session_state.current_engine == "claude":
        keyboard.append([InlineKeyboardButton("🔵 Cambiar a motor AGY", callback_data="cl_engine:agy")])
    else:
        keyboard.append([InlineKeyboardButton("🔵 Ver historial de AGY (/resume)", callback_data="res_page:0")])

    if not projects:
        text = (
            "🟠 **Sesiones de Claude Code por Proyecto**\n\n"
            "No se encontraron sesiones de proyectos en `~/.claude/projects/`."
        )
        return InlineKeyboardMarkup(keyboard), text

    cur_info = (
        f"• 📌 **Sesión Claude:** `{session_state.claude_session_title}`\n"
        f"• 🤖 **Modelo Claude:** `{session_state.claude_model or 'Default (Sonnet 4.6)'}`\n"
        if session_state.current_engine == "claude"
        else ""
    )
    text = (
        f"🟠 **Claude Code — Proyectos con Chats**\n"
        f"• ⚙️ **Estado:** {engine_badge}\n"
        f"{cur_info}\n"
        f"*Elige un proyecto para ver o continuar sus chats de Claude:*"
    )
    return InlineKeyboardMarkup(keyboard), text


def build_claude_sessions_keyboard(proj_idx: int, page: int = 0, page_size: int = 5) -> Tuple[Optional[InlineKeyboardMarkup], str]:
    projects = list_claude_projects()
    if not (0 <= proj_idx < len(projects)):
        return None, "❌ Proyecto de Claude no encontrado."

    proj = projects[proj_idx]
    slug = str(proj["slug"])
    sessions = list_claude_sessions_for_slug(slug)
    total = len(sessions)
    if total == 0:
        return None, f"❌ No hay chats guardados en `{proj['name']}`."

    total_pages = max(1, math.ceil(total / page_size))
    page = max(0, min(page, total_pages - 1))
    page_items = sessions[page * page_size : (page + 1) * page_size]

    keyboard = []
    for s in page_items:
        sid = s["session_id"]
        short_id = sid[:8]
        is_sel = (
            session_state.current_engine == "claude"
            and session_state.claude_session_id == sid
        )
        icon = "✅" if is_sel else "💬"
        title = s["title"]
        btn_label = f"{icon} {title[:34]}..." if len(title) > 34 else f"{icon} {title}"
        keyboard.append([
            InlineKeyboardButton(btn_label, callback_data=f"cl_sel:{proj_idx}:{short_id}")
        ])

    if total_pages > 1:
        nav = []
        if page > 0:
            nav.append(InlineKeyboardButton("◀️ Anterior", callback_data=f"cl_proj:{proj_idx}:{page - 1}"))
        nav.append(InlineKeyboardButton(f"📄 {page + 1}/{total_pages}", callback_data="res_noop"))
        if page < total_pages - 1:
            nav.append(InlineKeyboardButton("Siguiente ▶️", callback_data=f"cl_proj:{proj_idx}:{page + 1}"))
        keyboard.append(nav)

    keyboard.append([
        InlineKeyboardButton("🔄 Último (-c)", callback_data=f"cl_cont:{proj_idx}"),
        InlineKeyboardButton("➕ Nuevo Chat", callback_data=f"cl_new:{proj_idx}"),
        InlineKeyboardButton("🗑️ Borrar", callback_data=f"cl_del_page:{proj_idx}:0"),
    ])
    keyboard.append([
        InlineKeyboardButton("🔙 Proyectos Claude", callback_data="cl_home"),
        InlineKeyboardButton("🔵 Cambiar a AGY", callback_data="cl_engine:agy"),
    ])

    busy_warn = (
        "\n⚠️ *Nota: Tienes una sesión de Claude abierta en la terminal de este proyecto.*"
        if is_claude_busy_in_cwd(str(proj["cwd"]))
        else ""
    )

    text = (
        f"🟠 **Chats de Claude en `{proj['name']}`** ({total} sesiones)\n"
        f"📂 `{proj['cwd']}`{busy_warn}\n\n"
        f"*Toca una conversación para activarla y continuar escribiendo desde Telegram:*"
    )
    return InlineKeyboardMarkup(keyboard), text
