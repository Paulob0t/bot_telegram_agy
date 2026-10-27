import os
import re
import subprocess
from typing import Optional
from telegram import Update
from core.config import AUTHORIZED_USER_ID


def is_authorized(update: Update) -> bool:
    user = update.effective_user
    return user is not None and user.id == AUTHORIZED_USER_ID


async def safe_reply(message_obj, text: str, reply_markup=None):
    """Envía mensaje con Markdown, y si falla por formato, envía texto plano."""
    try:
        return await message_obj.reply_text(
            text,
            parse_mode="Markdown",
            reply_markup=reply_markup,
            disable_web_page_preview=True,
        )
    except Exception:
        return await message_obj.reply_text(
            text,
            reply_markup=reply_markup,
            disable_web_page_preview=True,
        )


async def safe_edit(msg_obj, text: str, reply_markup=None):
    """Edita mensaje con Markdown, y si falla por formato, edita en texto plano."""
    try:
        return await msg_obj.edit_text(
            text,
            parse_mode="Markdown",
            reply_markup=reply_markup,
            disable_web_page_preview=True,
        )
    except Exception:
        try:
            return await msg_obj.edit_text(
                text,
                reply_markup=reply_markup,
                disable_web_page_preview=True,
            )
        except Exception:
            pass


def format_uptime(seconds: int) -> str:
    if seconds < 60:
        return f"{seconds}s"
    elif seconds < 3600:
        return f"{seconds // 60}m {seconds % 60}s"
    else:
        return f"{seconds // 3600}h {(seconds % 3600) // 60}m"


def get_git_branch(path: str) -> Optional[str]:
    """Obtiene la rama de Git actual leyendo directamente .git/HEAD sin lanzar subprocesos."""
    if not path or not os.path.isdir(path):
        return None
    git_dir = os.path.join(path, ".git")
    head_file = os.path.join(git_dir, "HEAD")
    if os.path.isfile(head_file):
        try:
            with open(head_file, "r", encoding="utf-8") as f:
                ref = f.read().strip()
            if ref.startswith("ref: refs/heads/"):
                return ref[len("ref: refs/heads/"):]
            if ref:
                return ref[:7]
        except Exception:
            pass
    elif os.path.exists(git_dir):
        try:
            res = subprocess.run(
                ["git", "rev-parse", "--abbrev-ref", "HEAD"],
                cwd=path,
                capture_output=True,
                text=True,
                timeout=1.0,
            )
            if res.returncode == 0 and res.stdout.strip():
                return res.stdout.strip()
        except Exception:
            pass
    return None


def sanitize_streaming_markdown(text: str, max_len: int = 3800) -> str:
    """
    Trunca de forma segura y cierra temporalmente bloques de código (```),
    código inline (`) o negritas/cursivas abiertas durante el streaming para
    evitar que parse_mode="Markdown" de Telegram falle y parpadee a texto plano.
    """
    if not text:
        return ""

    truncated = text[:max_len]

    # 1. Evitar cortar justo en medio de un delimitador ``` al final del corte
    if truncated.endswith("``") and not truncated.endswith("```"):
        truncated = truncated[:-2]
    elif truncated.endswith("`") and not truncated.endswith("```"):
        # Solo quitar si era el inicio de un backtick recién abierto al borde
        pass

    # 2. Verificar si hay un bloque de código triple (```) sin cerrar
    fence_count = len(re.findall(r"```", truncated))
    if fence_count % 2 == 1:
        # El bloque ``` sigue abierto; cerrarlo limpiamente
        if not truncated.endswith("\n"):
            truncated += "\n"
        truncated += "```"
        return truncated

    # 3. Fuera de los bloques ``` cerrados, inspeccionar el segmento final
    last_fence_pos = truncated.rfind("```")
    tail = truncated[last_fence_pos + 3 :] if last_fence_pos != -1 else truncated

    # Contar backticks simples (`) en el segmento fuera de bloques de código
    inline_ticks = tail.count("`")
    if inline_ticks % 2 == 1:
        truncated += "`"
        return truncated

    # 4. Balancear negritas (**) o cursivas (*) abiertas fuera de código inline
    tail_no_code = re.sub(r"`[^`]*`", "", tail)
    bold_count = len(re.findall(r"\*\*", tail_no_code))
    if bold_count % 2 == 1:
        truncated += "**"
    else:
        remaining_stars = tail_no_code.replace("**", "").count("*")
        if remaining_stars % 2 == 1:
            truncated += "*"

    return truncated


def _clean_inline_snippet(val: str, max_chars: int = 60) -> str:
    clean = " ".join(str(val).replace("`", "'").split())
    if len(clean) > max_chars:
        return clean[: max_chars - 1] + "…"
    return clean


def format_step_tool_activity(step: dict) -> str:
    """
    Extrae una descripción rica de la herramienta o acción en curso a partir
    del evento `step_update` de AGY (comando ejecutado, archivo editado/leído,
    búsqueda, tool_action / tool_summary, etc.).
    """
    if not isinstance(step, dict):
        return ""

    sstate = str(step.get("state", "")).upper()
    is_done = "DONE" in sstate
    icon = "✅" if is_done else "⚙️"
    verb_prefix = "Completó" if is_done else "Ejecutando"

    # 1. Revisar acciones tipadas nativas del stream-json de AGY
    if "run_command" in step and isinstance(step["run_command"], dict):
        rc = step["run_command"]
        cmd = rc.get("command_line") or rc.get("command") or ""
        if cmd:
            snippet = _clean_inline_snippet(cmd, 65)
            label = "Comando completado" if is_done else "Ejecutando comando"
            return f"\n\n{icon} *{label}:* `{snippet}`"

    if "edit_file" in step and isinstance(step["edit_file"], dict):
        fp = step["edit_file"].get("file_path", "")
        fname = os.path.basename(fp) or fp
        label = "Archivo editado" if is_done else "Editando archivo"
        return f"\n\n{icon} *{label}:* `{_clean_inline_snippet(fname, 50)}`"

    if "create_file" in step and isinstance(step["create_file"], dict):
        fp = step["create_file"].get("file_path", "")
        fname = os.path.basename(fp) or fp
        label = "Archivo creado" if is_done else "Creando archivo"
        return f"\n\n{icon} *{label}:* `{_clean_inline_snippet(fname, 50)}`"

    if "view_file" in step and isinstance(step["view_file"], dict):
        fp = step["view_file"].get("file_path", "")
        fname = os.path.basename(fp) or fp
        label = "Archivo leído" if is_done else "Leyendo archivo"
        return f"\n\n{icon} *{label}:* `{_clean_inline_snippet(fname, 50)}`"

    if "list_directory" in step and isinstance(step["list_directory"], dict):
        dp = step["list_directory"].get("directory_path", "")
        dname = os.path.basename(dp.rstrip("/")) or dp
        label = "Directorio listado" if is_done else "Listando directorio"
        return f"\n\n{icon} *{label}:* `{_clean_inline_snippet(dname, 50)}`"

    if "find_file" in step and isinstance(step["find_file"], dict):
        q = step["find_file"].get("query") or step["find_file"].get("directory_path", "")
        label = "Búsqueda de archivos lista" if is_done else "Buscando archivos"
        return f"\n\n{icon} *{label}:* `{_clean_inline_snippet(q, 50)}`"

    if "search_directory" in step and isinstance(step["search_directory"], dict):
        q = step["search_directory"].get("query") or step["search_directory"].get("directory_path", "")
        label = "Búsqueda en código lista" if is_done else "Buscando en código"
        return f"\n\n{icon} *{label}:* `{_clean_inline_snippet(q, 50)}`"

    if "search_web" in step and isinstance(step["search_web"], dict):
        q = step["search_web"].get("query", "")
        label = "Búsqueda web lista" if is_done else "Buscando en la web"
        return f"\n\n{icon} *{label}:* `{_clean_inline_snippet(q, 55)}`"

    if "read_url_content" in step and isinstance(step["read_url_content"], dict):
        url = step["read_url_content"].get("url", "")
        label = "URL leída" if is_done else "Leyendo URL"
        return f"\n\n{icon} *{label}:* `{_clean_inline_snippet(url, 55)}`"

    # 2. Revisar metadatos de tool_action / tool_summary o argumentos de herramientas
    tname = step.get("tool_name") or ""
    tsummary = (
        step.get("tool_action")
        or step.get("toolAction")
        or step.get("tool_summary")
        or step.get("toolSummary")
        or ""
    )

    # Buscar dentro de custom_tool o arguments si vienen anidados
    args_dict = step.get("arguments") or step.get("args") or {}
    if not isinstance(args_dict, dict):
        args_dict = {}

    if not tsummary and "custom_tool" in step and isinstance(step["custom_tool"], dict):
        tc = step["custom_tool"].get("tool_call", {})
        if isinstance(tc, dict):
            tname = tname or tc.get("tool_name") or tc.get("name") or ""
            tc_args = tc.get("arguments") or {}
            if isinstance(tc_args, dict):
                args_dict = tc_args

    if not tsummary and args_dict:
        tsummary = (
            args_dict.get("toolAction")
            or args_dict.get("tool_action")
            or args_dict.get("toolSummary")
            or args_dict.get("tool_summary")
            or ""
        )

    # Extraer objetivo específico (archivo o comando) de argumentos o del propio step
    target_detail = (
        args_dict.get("CommandLine")
        or args_dict.get("command_line")
        or args_dict.get("TargetFile")
        or args_dict.get("AbsolutePath")
        or args_dict.get("file_path")
        or args_dict.get("query")
        or step.get("CommandLine")
        or step.get("command_line")
        or step.get("TargetFile")
        or step.get("AbsolutePath")
        or step.get("file_path")
        or ""
    )
    if target_detail and ("/" in str(target_detail)) and " " not in str(target_detail):
        target_detail = os.path.basename(str(target_detail).rstrip("/")) or target_detail

    if tsummary and target_detail:
        return f"\n\n{icon} *{tsummary}:* `{_clean_inline_snippet(target_detail, 50)}`"
    if tsummary:
        return f"\n\n{icon} *{tsummary}*" + (f" (`{tname}`)" if tname else "")
    if tname and target_detail:
        return f"\n\n{icon} *{verb_prefix} `{tname}`:* `{_clean_inline_snippet(target_detail, 50)}`"
    if tname:
        return f"\n\n{icon} *{verb_prefix} `{tname}`...*" if not is_done else f"\n\n{icon} *{verb_prefix} `{tname}`*"

    return ""


def _detect_unclosed_fence_lang(chunk_text: str) -> Optional[str]:
    """
    Analiza un fragmento de texto y determina si termina con un bloque de código
    triple (```) abierto.
    Retorna:
      - None si todos los bloques ``` están cerrados.
      - str (el identificador de lenguaje, ej. 'typescript', 'python', o '') si quedó abierto.
    """
    in_fence = False
    current_lang = ""
    for line in chunk_text.splitlines():
        stripped = line.strip()
        if stripped.startswith("```"):
            # Si la misma línea abre y cierra (ej. ```inline```) y tiene más de 3 caracteres después
            rest = stripped[3:]
            if "```" in rest:
                continue
            if not in_fence:
                in_fence = True
                # Extraer lenguaje (primera palabra tras ```)
                lang_token = rest.strip().split()[0] if rest.strip() else ""
                current_lang = re.sub(r"[^a-zA-Z0-9_\-\+#]", "", lang_token)
            else:
                in_fence = False
                current_lang = ""
    return current_lang if in_fence else None


def split_markdown_chunks(text: str, max_chars: int = 3900) -> list[str]:
    """
    Divide respuestas largas (> max_chars) respetando párrafos (\\n\\n), saltos
    de línea (\\n) o espacios, y si el corte cae dentro de un bloque de código
    (```lang), lo cierra al final del fragmento actual y lo reabre automáticamente
    con el mismo lenguaje al inicio del siguiente fragmento.
    """
    if not text:
        return [""]

    remaining = text.strip()
    if len(remaining) <= max_chars:
        return [remaining]

    chunks: list[str] = []
    # Margen de seguridad para añadir "\n```" al cerrar un bloque
    limit = max_chars - 20

    while remaining:
        if len(remaining) <= max_chars:
            chunks.append(remaining)
            break

        window = remaining[:limit]

        # 1. Priorizar corte en doble salto de línea (\n\n) en la mitad inferior de la ventana
        split_idx = window.rfind("\n\n", limit // 2)
        if split_idx != -1:
            cut_pos = split_idx
            next_start = split_idx + 2
        else:
            # 2. Priorizar corte en salto de línea simple (\n)
            split_idx = window.rfind("\n", limit // 3)
            if split_idx != -1:
                cut_pos = split_idx
                next_start = split_idx + 1
            else:
                # 3. Priorizar corte en espacio (' ') para no partir palabras a la mitad
                split_idx = window.rfind(" ", int(limit * 0.7))
                if split_idx != -1:
                    cut_pos = split_idx
                    next_start = split_idx + 1
                else:
                    # 4. Corte forzado si es una sola línea gigante sin espacios
                    cut_pos = limit
                    next_start = limit

        chunk = remaining[:cut_pos].rstrip()
        remaining = remaining[next_start:].lstrip("\n")

        # Verificar si el corte dejó un bloque de código ``` abierto
        open_lang = _detect_unclosed_fence_lang(chunk)
        if open_lang is not None:
            chunk = f"{chunk}\n```"
            remaining = f"```{open_lang}\n{remaining}"

        chunks.append(chunk)

    return chunks

