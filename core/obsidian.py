"""
Visor nativo de Obsidian y Lanzador de Prompts Técnicos (0 tokens para navegación/lectura).
Explora `/run/media/paulobot/Datos/Documents/wicode` (<Proyecto> / Notas | Prompts | PDF):
- Lectura paginada por secciones (`##`) sin consumir tokens.
- Búsqueda instantánea en toda la bóveda (`/obsidian <query>`).
- Toggle interactivo de checklists (`- [ ]` <-> `- [x]`) en las notas.
- Ejecución directa en 1 toque de cualquier prompt `T01..TXX` en Claude Code o AGY.
- Recepción de archivos `.pdf` en Telegram para correr el pipeline `wicode-prompt-generator`.
"""
import math
import os
import re
import time
import uuid
from typing import Dict, List, Optional, Tuple
from telegram import InlineKeyboardButton, InlineKeyboardMarkup
from core.config import OBSIDIAN_VAULT_DIR, PROJECTS_BASE_DIR
from core.state import session_state

# Mapeo de carpeta en Obsidian -> carpeta de código en Projects
PROJECT_CODE_MAP: Dict[str, str] = {
    "Stover": os.path.join(PROJECTS_BASE_DIR, "Stover"),
    "Wicode": os.path.join(PROJECTS_BASE_DIR, "wicode"),
}

# PDFs recién subidos esperando elegir proyecto: {hold_id: {"tmp_path": str, "filename": str, "ts": float}}
_PENDING_PDFS: Dict[str, Dict[str, object]] = {}


def list_vault_projects() -> List[Dict[str, object]]:
    """Lista los proyectos dentro de la bóveda Obsidian (/run/media/paulobot/Datos/Documents/wicode)."""
    if not os.path.isdir(OBSIDIAN_VAULT_DIR):
        return []
    projects: List[Dict[str, object]] = []
    for name in sorted(os.listdir(OBSIDIAN_VAULT_DIR)):
        if name.startswith("."):
            continue
        full = os.path.join(OBSIDIAN_VAULT_DIR, name)
        if not os.path.isdir(full):
            continue
        notas_dir = os.path.join(full, "Notas")
        prompts_dir = os.path.join(full, "Prompts")
        pdf_dir = os.path.join(full, "PDF")

        notas_count = len([f for f in os.listdir(notas_dir) if f.endswith(".md")]) if os.path.isdir(notas_dir) else 0
        pdf_count = len([f for f in os.listdir(pdf_dir) if f.lower().endswith(".pdf")]) if os.path.isdir(pdf_dir) else 0
        mod_count = (
            len([d for d in os.listdir(prompts_dir) if os.path.isdir(os.path.join(prompts_dir, d))])
            if os.path.isdir(prompts_dir)
            else 0
        )
        projects.append({
            "name": name,
            "path": full,
            "notas_count": notas_count,
            "mod_count": mod_count,
            "pdf_count": pdf_count,
        })
    return projects


def list_project_notes(proj_name: str) -> List[Tuple[str, str]]:
    """Retorna [(filename, full_path)] de Notas/*.md ordenados por mtime desc."""
    notas_dir = os.path.join(OBSIDIAN_VAULT_DIR, proj_name, "Notas")
    if not os.path.isdir(notas_dir):
        return []
    items = []
    for fn in os.listdir(notas_dir):
        if fn.endswith(".md"):
            full = os.path.join(notas_dir, fn)
            if os.path.isfile(full):
                items.append((fn, full, os.path.getmtime(full)))
    items.sort(key=lambda x: x[2], reverse=True)
    return [(fn, full) for fn, full, _ in items]


def list_project_pdfs(proj_name: str) -> List[Tuple[str, str]]:
    """Retorna [(filename, full_path)] de PDF/*.pdf ordenados por mtime desc."""
    pdf_dir = os.path.join(OBSIDIAN_VAULT_DIR, proj_name, "PDF")
    if not os.path.isdir(pdf_dir):
        return []
    items = []
    for fn in os.listdir(pdf_dir):
        if fn.lower().endswith(".pdf"):
            full = os.path.join(pdf_dir, fn)
            if os.path.isfile(full):
                items.append((fn, full, os.path.getmtime(full)))
    items.sort(key=lambda x: x[2], reverse=True)
    return [(fn, full) for fn, full, _ in items]


def list_prompt_modules(proj_name: str) -> List[Tuple[str, str, int]]:
    """Retorna [(mod_name, full_dir, count_files)] de Prompts/<Modulo>/."""
    prompts_dir = os.path.join(OBSIDIAN_VAULT_DIR, proj_name, "Prompts")
    if not os.path.isdir(prompts_dir):
        return []
    mods = []
    for d in sorted(os.listdir(prompts_dir)):
        full = os.path.join(prompts_dir, d)
        if os.path.isdir(full):
            files = [f for f in os.listdir(full) if f.endswith(".md")]
            mods.append((d, full, len(files)))
    return mods


def _natural_sort_key(filename: str):
    parts = re.split(r"(\d+)", filename)
    return [int(p) if p.isdigit() else p.lower() for p in parts]


def list_module_prompts(proj_name: str, mod_name: str) -> List[Tuple[str, str]]:
    """Retorna [(filename, full_path)] dentro de Prompts/<Modulo>/ con INDEX primero y T01..TXX ordenados."""
    mod_dir = os.path.join(OBSIDIAN_VAULT_DIR, proj_name, "Prompts", mod_name)
    if not os.path.isdir(mod_dir):
        return []
    files = [
        (fn, os.path.join(mod_dir, fn))
        for fn in os.listdir(mod_dir)
        if fn.endswith(".md") and os.path.isfile(os.path.join(mod_dir, fn))
    ]
    files.sort(key=lambda x: (0 if x[0].upper().startswith("INDEX") else 1, _natural_sort_key(x[0])))
    return files


def split_md_sections(md_path: str, max_chars: int = 3300) -> List[Tuple[str, str]]:
    """
    Divide un archivo Markdown en secciones navegables por encabezado (`## `)
    sin gastar tokens, respetando el límite de caracteres de Telegram.
    """
    if not os.path.isfile(md_path):
        return [("Vacío", "Archivo no encontrado.")]
    try:
        with open(md_path, "r", encoding="utf-8", errors="replace") as f:
            raw = f.read().strip()
    except Exception as e:
        return [("Error", f"No se pudo leer el archivo: {e}")]

    if not raw:
        return [("Vacío", "_(Archivo vacío)_")]

    # Si cabe completo en una sola vista, devolver 1 sección
    if len(raw) <= max_chars and not re.search(r"(?m)^###?\s+", raw):
        title = raw.splitlines()[0].lstrip("# ").strip()[:40] or "Documento"
        return [(title, raw)]

    blocks = re.split(r"(?m)^(?=###?\s+)", raw)
    sections: List[Tuple[str, str]] = []
    for idx, blk in enumerate(blocks):
        blk = blk.strip()
        if not blk:
            continue
        first_line = blk.splitlines()[0].lstrip("# ").strip()
        sec_title = first_line[:36] if first_line else f"Sección {idx + 1}"

        if len(blk) <= max_chars:
            sections.append((sec_title, blk))
        else:
            # Subdividir bloques largos por párrafos
            lines = blk.splitlines()
            chunk_lines: List[str] = []
            chunk_len = 0
            part_num = 1
            for line in lines:
                if chunk_len + len(line) + 1 > max_chars and chunk_lines:
                    sections.append((f"{sec_title} ({part_num})", "\n".join(chunk_lines)))
                    chunk_lines = [line]
                    chunk_len = len(line)
                    part_num += 1
                else:
                    chunk_lines.append(line)
                    chunk_len += len(line) + 1
            if chunk_lines:
                suffix = f" ({part_num})" if part_num > 1 else ""
                sections.append((f"{sec_title}{suffix}", "\n".join(chunk_lines)))

    return sections or [("Documento", raw[:max_chars])]


def extract_checklists(md_path: str) -> List[Tuple[int, bool, str]]:
    """Extrae [(line_idx, checked, text)] de todas las casillas `- [ ]` o `- [x]` de una nota."""
    if not os.path.isfile(md_path):
        return []
    items: List[Tuple[int, bool, str]] = []
    try:
        with open(md_path, "r", encoding="utf-8", errors="replace") as f:
            for idx, line in enumerate(f):
                m = re.match(r"^\s*-\s*\[([ xX])\]\s+(.+)$", line)
                if m:
                    checked = m.group(1).lower() == "x"
                    clean_label = re.sub(r"[*_`]", "", m.group(2)).strip()
                    items.append((idx, checked, clean_label))
    except Exception:
        pass
    return items


def toggle_checklist_line(md_path: str, line_idx: int) -> bool:
    """Alterna `- [ ]` <-> `- [x]` en la línea indicada del archivo Markdown."""
    if not os.path.isfile(md_path):
        return False
    try:
        with open(md_path, "r", encoding="utf-8") as f:
            lines = f.readlines()
        if not (0 <= line_idx < len(lines)):
            return False
        target = lines[line_idx]
        if "- [ ]" in target:
            lines[line_idx] = target.replace("- [ ]", "- [x]", 1)
        elif "- [x]" in target:
            lines[line_idx] = target.replace("- [x]", "- [ ]", 1)
        elif "- [X]" in target:
            lines[line_idx] = target.replace("- [X]", "- [ ]", 1)
        else:
            return False
        with open(md_path, "w", encoding="utf-8") as f:
            f.writelines(lines)
        return True
    except Exception:
        return False


def build_vault_home_keyboard() -> Tuple[InlineKeyboardMarkup, str]:
    projects = list_vault_projects()
    keyboard: List[List[InlineKeyboardButton]] = []

    for idx, p in enumerate(projects):
        keyboard.append([
            InlineKeyboardButton(
                f"🟣 {p['name']} ({p['notas_count']} notas · {p['mod_count']} módulos · {p['pdf_count']} PDFs)",
                callback_data=f"ob_proj:{idx}",
            )
        ])

    text = (
        "🟣 **Bóveda Obsidian & Prompts Wicode (0 Tokens)**\n"
        f"📂 `{OBSIDIAN_VAULT_DIR}`\n\n"
        "• Lee tus notas de contexto `.md`, checklists de dudas y prompts `T01..TXX` sin gastar tokens.\n"
        "• Usa `/obsidian <palabra>` para buscar en toda la bóveda.\n"
        "• Envía un archivo `.pdf` al chat para transcribirlo y generar sus prompts automáticamente.\n\n"
        "*Selecciona un proyecto:*"
    )
    return InlineKeyboardMarkup(keyboard), text


def build_vault_project_keyboard(proj_idx: int) -> Tuple[Optional[InlineKeyboardMarkup], str]:
    projects = list_vault_projects()
    if not (0 <= proj_idx < len(projects)):
        return None, "❌ Proyecto no encontrado en la bóveda."

    p = projects[proj_idx]
    keyboard = [
        [
            InlineKeyboardButton(f"📒 Notas ({p['notas_count']})", callback_data=f"ob_notes:{proj_idx}:0"),
            InlineKeyboardButton(f"🎯 Prompts ({p['mod_count']} mód.)", callback_data=f"ob_mods:{proj_idx}"),
        ],
        [
            InlineKeyboardButton(f"📄 PDFs ({p['pdf_count']})", callback_data=f"ob_pdfs:{proj_idx}:0"),
        ],
        [
            InlineKeyboardButton("🔙 Volver a Bóveda", callback_data="ob_home"),
        ],
    ]
    text = (
        f"🟣 **Obsidian — Proyecto `{p['name']}`**\n"
        f"📂 `{p['path']}`\n\n"
        f"• 📒 **Notas de Contexto:** `{p['notas_count']}` archivos `.md`\n"
        f"• 🎯 **Módulos de Prompts:** `{p['mod_count']}` carpetas (`T01..TXX`)\n"
        f"• 📄 **Documentos PDF:** `{p['pdf_count']}` especificaciones\n\n"
        "*¿Qué deseas abrir?*"
    )
    return InlineKeyboardMarkup(keyboard), text


def build_notes_list_keyboard(proj_idx: int, page: int = 0, page_size: int = 6) -> Tuple[Optional[InlineKeyboardMarkup], str]:
    projects = list_vault_projects()
    if not (0 <= proj_idx < len(projects)):
        return None, "❌ Proyecto no encontrado."
    proj_name = str(projects[proj_idx]["name"])
    notes = list_project_notes(proj_name)
    total = len(notes)
    if total == 0:
        return (
            InlineKeyboardMarkup([[InlineKeyboardButton("🔙 Volver", callback_data=f"ob_proj:{proj_idx}")]]),
            f"ℹ️ No hay notas `.md` en `{proj_name}/Notas/`.",
        )

    total_pages = max(1, math.ceil(total / page_size))
    page = max(0, min(page, total_pages - 1))
    page_items = notes[page * page_size : (page + 1) * page_size]

    keyboard = []
    for i, (fn, _) in enumerate(page_items):
        abs_idx = page * page_size + i
        clean = fn[:-3] if fn.endswith(".md") else fn
        label = f"📒 {clean[:36]}..." if len(clean) > 36 else f"📒 {clean}"
        keyboard.append([InlineKeyboardButton(label, callback_data=f"ob_nview:{proj_idx}:{abs_idx}:0")])

    if total_pages > 1:
        nav = []
        if page > 0:
            nav.append(InlineKeyboardButton("◀️ Anterior", callback_data=f"ob_notes:{proj_idx}:{page - 1}"))
        nav.append(InlineKeyboardButton(f"📄 {page + 1}/{total_pages}", callback_data="res_noop"))
        if page < total_pages - 1:
            nav.append(InlineKeyboardButton("Siguiente ▶️", callback_data=f"ob_notes:{proj_idx}:{page + 1}"))
        keyboard.append(nav)

    keyboard.append([InlineKeyboardButton(f"🔙 Volver a {proj_name}", callback_data=f"ob_proj:{proj_idx}")])
    text = f"📒 **Notas de Obsidian en `{proj_name}`** ({total} notas)\n*Toca una nota para leerla por secciones (0 tokens):*"
    return InlineKeyboardMarkup(keyboard), text


def build_note_reader_view(proj_idx: int, note_idx: int, sec_idx: int = 0) -> Tuple[Optional[InlineKeyboardMarkup], str, Optional[str]]:
    """Retorna (markup, text, file_path) para leer una sección de una nota."""
    projects = list_vault_projects()
    if not (0 <= proj_idx < len(projects)):
        return None, "❌ Proyecto no encontrado.", None
    proj_name = str(projects[proj_idx]["name"])
    notes = list_project_notes(proj_name)
    if not (0 <= note_idx < len(notes)):
        return None, "❌ Nota no encontrada.", None

    fn, full_path = notes[note_idx]
    sections = split_md_sections(full_path)
    total_secs = len(sections)
    sec_idx = max(0, min(sec_idx, total_secs - 1))
    sec_title, sec_body = sections[sec_idx]

    keyboard: List[List[InlineKeyboardButton]] = []

    # Navegación de secciones
    if total_secs > 1:
        nav = []
        if sec_idx > 0:
            nav.append(InlineKeyboardButton("◀️ Sec. Ant.", callback_data=f"ob_nview:{proj_idx}:{note_idx}:{sec_idx - 1}"))
        nav.append(InlineKeyboardButton(f"📑 {sec_idx + 1}/{total_secs}", callback_data="res_noop"))
        if sec_idx < total_secs - 1:
            nav.append(InlineKeyboardButton("Sec. Sig. ▶️", callback_data=f"ob_nview:{proj_idx}:{note_idx}:{sec_idx + 1}"))
        keyboard.append(nav)

        # Botones directos a las secciones principales (máx 6 botones en filas de 2)
        sec_btns: List[InlineKeyboardButton] = []
        for s_i, (s_t, _) in enumerate(sections[:8]):
            mark = "📍" if s_i == sec_idx else "🔹"
            short_t = s_t[:16]
            sec_btns.append(InlineKeyboardButton(f"{mark} {short_t}", callback_data=f"ob_nview:{proj_idx}:{note_idx}:{s_i}"))
            if len(sec_btns) == 2:
                keyboard.append(sec_btns)
                sec_btns = []
        if sec_btns:
            keyboard.append(sec_btns)

    checks = extract_checklists(full_path)
    action_row = [
        InlineKeyboardButton("📥 Enviar .md", callback_data=f"ob_ndoc:{proj_idx}:{note_idx}"),
    ]
    if checks:
        done_c = sum(1 for _, c, _ in checks if c)
        action_row.append(
            InlineKeyboardButton(f"☑️ Dudas ({done_c}/{len(checks)})", callback_data=f"ob_nchk:{proj_idx}:{note_idx}:0")
        )
    keyboard.append(action_row)
    keyboard.append([InlineKeyboardButton("🔙 Lista de Notas", callback_data=f"ob_notes:{proj_idx}:0")])

    header = f"📒 **`{proj_name}/Notas/{fn}`** — *[{sec_idx + 1}/{total_secs}: {sec_title}]*\n\n"
    return InlineKeyboardMarkup(keyboard), header + sec_body, full_path


def build_note_checklist_view(proj_idx: int, note_idx: int, page: int = 0, page_size: int = 6) -> Tuple[Optional[InlineKeyboardMarkup], str]:
    projects = list_vault_projects()
    if not (0 <= proj_idx < len(projects)):
        return None, "❌ Proyecto no encontrado."
    proj_name = str(projects[proj_idx]["name"])
    notes = list_project_notes(proj_name)
    if not (0 <= note_idx < len(notes)):
        return None, "❌ Nota no encontrada."

    fn, full_path = notes[note_idx]
    checks = extract_checklists(full_path)
    total = len(checks)
    if total == 0:
        return None, "ℹ️ Esta nota no tiene casillas `- [ ]`."

    total_pages = max(1, math.ceil(total / page_size))
    page = max(0, min(page, total_pages - 1))
    slice_items = checks[page * page_size : (page + 1) * page_size]

    keyboard = []
    for line_idx, is_checked, label in slice_items:
        icon = "✅" if is_checked else "⬜"
        short_lbl = label[:34] + ("..." if len(label) > 34 else "")
        keyboard.append([
            InlineKeyboardButton(f"{icon} {short_lbl}", callback_data=f"ob_ntog:{proj_idx}:{note_idx}:{line_idx}:{page}")
        ])

    if total_pages > 1:
        nav = []
        if page > 0:
            nav.append(InlineKeyboardButton("◀️ Anterior", callback_data=f"ob_nchk:{proj_idx}:{note_idx}:{page - 1}"))
        nav.append(InlineKeyboardButton(f"📄 {page + 1}/{total_pages}", callback_data="res_noop"))
        if page < total_pages - 1:
            nav.append(InlineKeyboardButton("Siguiente ▶️", callback_data=f"ob_nchk:{proj_idx}:{note_idx}:{page + 1}"))
        keyboard.append(nav)

    keyboard.append([InlineKeyboardButton("🔙 Volver a la Nota", callback_data=f"ob_nview:{proj_idx}:{note_idx}:0")])
    done_c = sum(1 for _, c, _ in checks if c)
    text = (
        f"☑️ **Checklist en `{fn}`** ({done_c}/{total} completadas)\n"
        "*Toca cualquier ítem para marcarlo o desmarcarlo directamente en tu archivo Obsidian (0 tokens):*"
    )
    return InlineKeyboardMarkup(keyboard), text


def build_prompt_modules_keyboard(proj_idx: int) -> Tuple[Optional[InlineKeyboardMarkup], str]:
    projects = list_vault_projects()
    if not (0 <= proj_idx < len(projects)):
        return None, "❌ Proyecto no encontrado."
    proj_name = str(projects[proj_idx]["name"])
    mods = list_prompt_modules(proj_name)
    if not mods:
        return (
            InlineKeyboardMarkup([[InlineKeyboardButton("🔙 Volver", callback_data=f"ob_proj:{proj_idx}")]]),
            f"ℹ️ No hay carpetas de prompts en `{proj_name}/Prompts/`.",
        )

    keyboard = []
    for m_idx, (mod_name, _, count) in enumerate(mods):
        keyboard.append([
            InlineKeyboardButton(f"🎯 {mod_name} ({count} archivos)", callback_data=f"ob_mlist:{proj_idx}:{m_idx}:0")
        ])
    keyboard.append([InlineKeyboardButton(f"🔙 Volver a {proj_name}", callback_data=f"ob_proj:{proj_idx}")])
    text = f"🎯 **Módulos de Prompts Técnicos en `{proj_name}`**\n*Selecciona un módulo para ver sus tareas `T01..TXX`:*"
    return InlineKeyboardMarkup(keyboard), text


def build_module_prompts_keyboard(proj_idx: int, mod_idx: int, page: int = 0, page_size: int = 7) -> Tuple[Optional[InlineKeyboardMarkup], str]:
    projects = list_vault_projects()
    if not (0 <= proj_idx < len(projects)):
        return None, "❌ Proyecto no encontrado."
    proj_name = str(projects[proj_idx]["name"])
    mods = list_prompt_modules(proj_name)
    if not (0 <= mod_idx < len(mods)):
        return None, "❌ Módulo no encontrado."

    mod_name, _, _ = mods[mod_idx]
    files = list_module_prompts(proj_name, mod_name)
    total = len(files)
    if total == 0:
        return None, f"ℹ️ No hay prompts en `{mod_name}`."

    total_pages = max(1, math.ceil(total / page_size))
    page = max(0, min(page, total_pages - 1))
    slice_files = files[page * page_size : (page + 1) * page_size]

    keyboard = []
    for i, (fn, _) in enumerate(slice_files):
        abs_idx = page * page_size + i
        clean = fn[:-3] if fn.endswith(".md") else fn
        if clean.upper().startswith("INDEX"):
            keyboard.append([
                InlineKeyboardButton(f"📋 Leer {clean} (0 tokens)", callback_data=f"ob_pview:{proj_idx}:{mod_idx}:{abs_idx}:0")
            ])
        else:
            short_c = clean[:28] + ("..." if len(clean) > 28 else "")
            keyboard.append([
                InlineKeyboardButton(f"📖 {short_c}", callback_data=f"ob_pview:{proj_idx}:{mod_idx}:{abs_idx}:0"),
                InlineKeyboardButton("🚀 Lanzar", callback_data=f"ob_pask:{proj_idx}:{mod_idx}:{abs_idx}"),
            ])

    if total_pages > 1:
        nav = []
        if page > 0:
            nav.append(InlineKeyboardButton("◀️ Anterior", callback_data=f"ob_mlist:{proj_idx}:{mod_idx}:{page - 1}"))
        nav.append(InlineKeyboardButton(f"📄 {page + 1}/{total_pages}", callback_data="res_noop"))
        if page < total_pages - 1:
            nav.append(InlineKeyboardButton("Siguiente ▶️", callback_data=f"ob_mlist:{proj_idx}:{mod_idx}:{page + 1}"))
        keyboard.append(nav)

    keyboard.append([
        InlineKeyboardButton("🔙 Módulos", callback_data=f"ob_mods:{proj_idx}"),
        InlineKeyboardButton(f"📒 Notas de {proj_name}", callback_data=f"ob_notes:{proj_idx}:0"),
    ])
    text = (
        f"🎯 **Prompts de `{proj_name} / {mod_name}`** ({total} archivos)\n"
        "• Toca **📖** para **leer el `.md` por secciones** o descargarlo (**0 tokens**).\n"
        "• Toca **🚀 Lanzar** solo si deseas mandarlo a ejecutar en Claude / AGY."
    )
    return InlineKeyboardMarkup(keyboard), text


def build_prompt_reader_view(proj_idx: int, mod_idx: int, file_idx: int, sec_idx: int = 0) -> Tuple[Optional[InlineKeyboardMarkup], str, Optional[str]]:
    projects = list_vault_projects()
    if not (0 <= proj_idx < len(projects)):
        return None, "❌ Proyecto no encontrado.", None
    proj_name = str(projects[proj_idx]["name"])
    mods = list_prompt_modules(proj_name)
    if not (0 <= mod_idx < len(mods)):
        return None, "❌ Módulo no encontrado.", None
    mod_name, _, _ = mods[mod_idx]
    files = list_module_prompts(proj_name, mod_name)
    if not (0 <= file_idx < len(files)):
        return None, "❌ Prompt no encontrado.", None

    fn, full_path = files[file_idx]
    sections = split_md_sections(full_path)
    total_secs = len(sections)
    sec_idx = max(0, min(sec_idx, total_secs - 1))
    sec_title, sec_body = sections[sec_idx]

    keyboard: List[List[InlineKeyboardButton]] = []
    if total_secs > 1:
        nav = []
        if sec_idx > 0:
            nav.append(InlineKeyboardButton("◀️ Sec. Ant.", callback_data=f"ob_pview:{proj_idx}:{mod_idx}:{file_idx}:{sec_idx - 1}"))
        nav.append(InlineKeyboardButton(f"📑 {sec_idx + 1}/{total_secs}", callback_data="res_noop"))
        if sec_idx < total_secs - 1:
            nav.append(InlineKeyboardButton("Sec. Sig. ▶️", callback_data=f"ob_pview:{proj_idx}:{mod_idx}:{file_idx}:{sec_idx + 1}"))
        keyboard.append(nav)

        sec_btns: List[InlineKeyboardButton] = []
        for s_i, (s_t, _) in enumerate(sections[:6]):
            mark = "📍" if s_i == sec_idx else "🔹"
            sec_btns.append(
                InlineKeyboardButton(f"{mark} {s_t[:16]}", callback_data=f"ob_pview:{proj_idx}:{mod_idx}:{file_idx}:{s_i}")
            )
            if len(sec_btns) == 2:
                keyboard.append(sec_btns)
                sec_btns = []
        if sec_btns:
            keyboard.append(sec_btns)

    action_row = [
        InlineKeyboardButton("📥 Enviar .md", callback_data=f"ob_pdoc:{proj_idx}:{mod_idx}:{file_idx}"),
    ]
    if not fn.upper().startswith("INDEX"):
        action_row.append(
            InlineKeyboardButton("🚀 Ejecutar...", callback_data=f"ob_pask:{proj_idx}:{mod_idx}:{file_idx}")
        )
    keyboard.append(action_row)

    # Navegación rápida al siguiente prompt T(X+1)
    row_bottom = [InlineKeyboardButton("🔙 Lista T01..TXX", callback_data=f"ob_mlist:{proj_idx}:{mod_idx}:0")]
    if file_idx + 1 < len(files):
        next_fn = files[file_idx + 1][0].split("-")[0]
        row_bottom.append(
            InlineKeyboardButton(f"⏭️ Leer {next_fn}", callback_data=f"ob_pview:{proj_idx}:{mod_idx}:{file_idx + 1}:0")
        )
    keyboard.append(row_bottom)

    header = f"📖 **`{proj_name}/Prompts/{mod_name}/{fn}`** — *[{sec_idx + 1}/{total_secs}: {sec_title}]*\n\n"
    return InlineKeyboardMarkup(keyboard), header + sec_body, full_path


def build_prompt_launch_confirm(proj_idx: int, mod_idx: int, file_idx: int) -> Tuple[Optional[InlineKeyboardMarkup], str]:
    projects = list_vault_projects()
    if not (0 <= proj_idx < len(projects)):
        return None, "❌ Proyecto no encontrado."
    proj_name = str(projects[proj_idx]["name"])
    mods = list_prompt_modules(proj_name)
    if not (0 <= mod_idx < len(mods)):
        return None, "❌ Módulo no encontrado."
    mod_name = mods[mod_idx][0]
    files = list_module_prompts(proj_name, mod_name)
    if not (0 <= file_idx < len(files)):
        return None, "❌ Prompt no encontrado."
    fn, _ = files[file_idx]
    target_cwd = PROJECT_CODE_MAP.get(proj_name, session_state.current_cwd)
    keyboard = InlineKeyboardMarkup([
        [
            InlineKeyboardButton("🟠 Ejecutar en Claude Code", callback_data=f"ob_prun:cl:{proj_idx}:{mod_idx}:{file_idx}"),
            InlineKeyboardButton("🔵 Ejecutar en AGY", callback_data=f"ob_prun:ag:{proj_idx}:{mod_idx}:{file_idx}"),
        ],
        [
            InlineKeyboardButton("📖 Leer Prompt (0 tokens)", callback_data=f"ob_pview:{proj_idx}:{mod_idx}:{file_idx}:0"),
            InlineKeyboardButton("🔙 Volver", callback_data=f"ob_mlist:{proj_idx}:{mod_idx}:0"),
        ],
    ])
    text = (
        f"🚀 **¿Lanzar tarea `{fn}`?**\n\n"
        f"• 📂 **Módulo:** `{proj_name} / {mod_name}`\n"
        f"• 💻 **Repositorio destino:** `{target_cwd}`\n\n"
        "*Elige en qué motor deseas ejecutarla (o toca 📖 para solo leerla con 0 tokens):*"
    )
    return keyboard, text


def build_pdfs_list_keyboard(proj_idx: int, page: int = 0, page_size: int = 6) -> Tuple[Optional[InlineKeyboardMarkup], str]:
    projects = list_vault_projects()
    if not (0 <= proj_idx < len(projects)):
        return None, "❌ Proyecto no encontrado."
    proj_name = str(projects[proj_idx]["name"])
    pdfs = list_project_pdfs(proj_name)
    total = len(pdfs)
    if total == 0:
        return (
            InlineKeyboardMarkup([[InlineKeyboardButton("🔙 Volver", callback_data=f"ob_proj:{proj_idx}")]]),
            f"ℹ️ No hay archivos PDF en `{proj_name}/PDF/`.",
        )

    total_pages = max(1, math.ceil(total / page_size))
    page = max(0, min(page, total_pages - 1))
    slice_pdfs = pdfs[page * page_size : (page + 1) * page_size]

    keyboard = []
    for i, (fn, _) in enumerate(slice_pdfs):
        abs_idx = page * page_size + i
        short_fn = fn[:28] + ("..." if len(fn) > 28 else "")
        keyboard.append([
            InlineKeyboardButton(f"📥 {short_fn}", callback_data=f"ob_pdf_dl:{proj_idx}:{abs_idx}"),
            InlineKeyboardButton("⚡ Generar Prompts", callback_data=f"ob_pdf_gen:{proj_idx}:{abs_idx}"),
        ])

    if total_pages > 1:
        nav = []
        if page > 0:
            nav.append(InlineKeyboardButton("◀️ Anterior", callback_data=f"ob_pdfs:{proj_idx}:{page - 1}"))
        nav.append(InlineKeyboardButton(f"📄 {page + 1}/{total_pages}", callback_data="res_noop"))
        if page < total_pages - 1:
            nav.append(InlineKeyboardButton("Siguiente ▶️", callback_data=f"ob_pdfs:{proj_idx}:{page + 1}"))
        keyboard.append(nav)

    keyboard.append([InlineKeyboardButton(f"🔙 Volver a {proj_name}", callback_data=f"ob_proj:{proj_idx}")])
    text = (
        f"📄 **PDFs en `{proj_name}/PDF/`** ({total} archivos)\n"
        "• Toca **📥** para enviarte el PDF por Telegram (0 tokens).\n"
        "• Toca **⚡ Generar Prompts** para correr la skill `wicode-prompt-generator` sobre ese PDF."
    )
    return InlineKeyboardMarkup(keyboard), text


def search_vault(query: str, max_results: int = 8) -> Tuple[InlineKeyboardMarkup, str]:
    """Busca texto en todos los .md de la bóveda con 0 consumo de tokens."""
    q = query.strip().lower()
    projects = list_vault_projects()
    buttons: List[List[InlineKeyboardButton]] = []
    matches_text: List[str] = []

    for p_idx, p in enumerate(projects):
        proj_name = str(p["name"])
        for n_idx, (fn, full) in enumerate(list_project_notes(proj_name)):
            try:
                with open(full, "r", encoding="utf-8", errors="replace") as f:
                    content = f.read()
                if q in fn.lower() or q in content.lower():
                    snippet = ""
                    for line in content.splitlines():
                        if q in line.lower():
                            snippet = line.strip()[:70]
                            break
                    matches_text.append(f"• 📒 `{proj_name}/Notas/{fn}`" + (f"\n  _{snippet}_" if snippet else ""))
                    buttons.append([
                        InlineKeyboardButton(f"📒 {proj_name}: {fn[:28]}", callback_data=f"ob_nview:{p_idx}:{n_idx}:0")
                    ])
                    if len(buttons) >= max_results:
                        break
            except Exception:
                continue
        if len(buttons) >= max_results:
            break

        for m_idx, (mod_name, _, _) in enumerate(list_prompt_modules(proj_name)):
            for f_idx, (fn, full) in enumerate(list_module_prompts(proj_name, mod_name)):
                try:
                    with open(full, "r", encoding="utf-8", errors="replace") as f:
                        content = f.read()
                    if q in fn.lower() or q in content.lower():
                        matches_text.append(f"• 🎯 `{proj_name}/{mod_name}/{fn}`")
                        buttons.append([
                            InlineKeyboardButton(
                                f"🎯 {mod_name}: {fn[:24]}",
                                callback_data=f"ob_pview:{p_idx}:{m_idx}:{f_idx}:0",
                            )
                        ])
                        if len(buttons) >= max_results:
                            break
                except Exception:
                    continue
            if len(buttons) >= max_results:
                break

    buttons.append([InlineKeyboardButton("🟣 Abrir Bóveda Obsidian", callback_data="ob_home")])
    if not matches_text:
        return InlineKeyboardMarkup(buttons), f"🔎 No se encontraron coincidencias para `{query}` en la bóveda."

    body = "\n".join(matches_text)
    return (
        InlineKeyboardMarkup(buttons),
        f"🔎 **Resultados en Obsidian para `{query}` (0 tokens):**\n\n{body}\n\n*Toca uno para abrirlo:*",
    )


def register_pending_pdf(tmp_path: str, filename: str) -> Tuple[str, InlineKeyboardMarkup, str]:
    """Registra un PDF subido al chat y devuelve el teclado para elegir en qué proyecto guardarlo."""
    now = time.time()
    for k in [k for k, v in _PENDING_PDFS.items() if now - float(v["ts"]) > 3600]:
        old = _PENDING_PDFS.pop(k, None)
        if old and os.path.exists(str(old["tmp_path"])):
            try:
                os.remove(str(old["tmp_path"]))
            except Exception:
                pass

    hold_id = uuid.uuid4().hex[:8]
    _PENDING_PDFS[hold_id] = {"tmp_path": tmp_path, "filename": filename, "ts": now}

    projects = list_vault_projects()
    keyboard = []
    for idx, p in enumerate(projects):
        pname = str(p["name"])
        keyboard.append([
            InlineKeyboardButton(
                f"⚡ Guardar en {pname} y Generar Nota + Prompts",
                callback_data=f"ob_pdf_up:{hold_id}:{idx}:gen",
            )
        ])
        keyboard.append([
            InlineKeyboardButton(
                f"📥 Solo guardar en {pname}/PDF/ (0 tokens)",
                callback_data=f"ob_pdf_up:{hold_id}:{idx}:save",
            )
        ])
    keyboard.append([InlineKeyboardButton("🗑️ Descartar PDF", callback_data=f"ob_pdf_up:{hold_id}:0:drop")])

    text = (
        f"📄 **PDF recibido:** `{filename}`\n\n"
        "¿En qué proyecto de la bóveda Obsidian deseas guardarlo o procesarlo con `wicode-prompt-generator`?"
    )
    return hold_id, InlineKeyboardMarkup(keyboard), text


def pop_pending_pdf(hold_id: str) -> Optional[Dict[str, object]]:
    return _PENDING_PDFS.pop(hold_id, None)


def build_wicode_generator_prompt(proj_name: str, pdf_dest_path: str) -> Tuple[str, str]:
    """Construye el prompt para ejecutar el skill `wicode-prompt-generator` sobre un PDF."""
    mod_slug = os.path.splitext(os.path.basename(pdf_dest_path))[0]
    target_cwd = PROJECT_CODE_MAP.get(proj_name, session_state.current_cwd)
    if not os.path.isdir(target_cwd):
        target_cwd = session_state.current_cwd
    prompt = (
        f"Usa la skill `wicode-prompt-generator` para procesar el PDF `{pdf_dest_path}` del proyecto `{proj_name}`.\n"
        f"1. Fase 1: Genera la nota completa de contexto en `/run/media/paulobot/Datos/Documents/wicode/{proj_name}/Notas/{mod_slug}.md`.\n"
        f"2. Fase 2: Inspecciona el código real y genera todos los prompts técnicos `T01..TXX` en `/run/media/paulobot/Datos/Documents/wicode/{proj_name}/Prompts/{mod_slug}/`."
    )
    return prompt, target_cwd
