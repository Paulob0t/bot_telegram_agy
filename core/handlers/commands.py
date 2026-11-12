import os
import shutil
import time
import tempfile
from telegram import BotCommand, InlineKeyboardButton, InlineKeyboardMarkup, Update
from telegram.ext import ContextTypes
from core.config import HOME_DIR, IS_WINDOWS, logger
from core.state import session_state
from core.utils import is_authorized, safe_reply, safe_edit, format_uptime, get_git_branch
from core.chats_db import build_resume_keyboard, build_delete_keyboard
from core.projects import (
    build_projects_keyboard,
    resolve_project_Target,
    format_active_cwd_summary,
    list_available_projects,
)
from core.models import (
    build_models_keyboard,
    build_ai_quotas_dashboard,
    get_effective_model_display,
    get_models_catalog,
    sync_models_from_cli,
    get_context_window_for_model,
)
from core.desktop_tools import (
    capture_wayland_screenshot,
    build_screenshot_keyboard,
)
from core.claude_chats import (
    build_claude_projects_keyboard,
    build_claude_sessions_keyboard,
    build_claude_delete_keyboard,
    list_claude_projects,
    get_claude_slug_for_cwd,
)
from core.obsidian import (
    build_vault_home_keyboard,
    build_vault_project_keyboard,
    build_prompt_modules_keyboard,
    list_vault_projects,
    search_vault,
)
from core.executor import execution_manager
from core.chat_guard import chat_guard


def get_active_bot_commands():
    commands = [
        BotCommand("start", "Panel principal y ayuda"),
        BotCommand("config", "Activar o desactivar módulos del bot"),
    ]
    if session_state.is_feature_enabled("obsidian"):
        commands.append(BotCommand("obsidian", "Explorar bóveda Obsidian .md y PDFs (0 tokens)"))
        commands.append(BotCommand("prompts", "Ver y lanzar prompts técnicos T01..TXX"))
    if session_state.is_feature_enabled("claude"):
        commands.append(BotCommand("claude", "Proyectos y chats de Claude Code"))
        commands.append(BotCommand("agy", "Cambiar motor a Antigravity (AGY)"))
    commands.extend([
        BotCommand("project", "Cambiar proyecto o repositorio (cwd)"),
        BotCommand("resume", "Historial de chats y cambiar conversación"),
        BotCommand("new", "Iniciar nuevo chat limpio"),
        BotCommand("continue", "Continuar última conversación activa"),
        BotCommand("current", "Ver proyecto, chat y modelo activos"),
        BotCommand("model", "Cambiar o sincronizar modelo de IA"),
        BotCommand("effort", "Ajustar nivel de razonamiento (low..max)"),
        BotCommand("usage", "Ver capacidad y consumo de tokens"),
        BotCommand("status", "Estado de AGY, RAM, disco y cola"),
        BotCommand("queue", "Ver o gestionar cola de mensajes"),
        BotCommand("cancel", "Cancelar ejecución actual"),
    ])
    if session_state.is_feature_enabled("screen"):
        commands.append(BotCommand("screen", "Captura de pantalla de la PC"))
    commands.extend([
        BotCommand("delete", "Eliminar conversaciones guardadas"),
        BotCommand("off", "Apagar servicio del bot"),
    ])
    return commands


async def sync_telegram_commands_menu(bot) -> None:
    commands = get_active_bot_commands()
    try:
        await bot.set_my_commands(commands)
        logger.info("Menú de comandos de Telegram actualizado (%d comandos).", len(commands))
    except Exception as e:
        logger.warning("No se pudo actualizar el menú de comandos: %s", e)


def build_config_view():
    items = [
        ("obsidian", "🟣 Obsidian & Prompts (/obsidian, /prompts)"),
        ("claude", "🟠 Motor Claude Code (/claude, /agy)"),
        ("screen", "📸 Captura de Pantalla (/screen)"),
        ("chat_guard", "🛡️ Guardia de Inactividad (ChatGuard)"),
    ]
    keyboard = []
    lines = [
        "⚙️ **Configuración de Módulos del Bot (`/config`)**\n",
        "Activa o desactiva las herramientas que usas. El menú de comandos `/` y `/start` se adaptan al instante:\n",
    ]
    for key, label in items:
        enabled = session_state.is_feature_enabled(key)
        status_icon = "✅" if enabled else "❌"
        state_txt = "Activo" if enabled else "Desactivado"
        lines.append(f"• {status_icon} **{label}:** `{state_txt}`")
        keyboard.append([
            InlineKeyboardButton(f"{status_icon} {label}", callback_data=f"cfg_tog:{key}")
        ])
    return InlineKeyboardMarkup(keyboard), "\n".join(lines)


async def cmd_config(update: Update, context: ContextTypes.DEFAULT_TYPE):
    if not is_authorized(update):
        return
    markup, text = build_config_view()
    await safe_reply(update.message, text, reply_markup=markup)


async def cmd_start(update: Update, context: ContextTypes.DEFAULT_TYPE):
    if not is_authorized(update):
        return
    user = update.effective_user
    first_name = (user.first_name if user and user.first_name else "Developer").strip()
    model_disp, _ = get_effective_model_display()
    cwd_short = session_state.get_cwd_short_name()
    branch = get_git_branch(session_state.current_cwd)
    branch_str = f" (`{branch}`)" if branch else ""
    os_label = "Windows" if IS_WINDOWS else "Linux"

    sections = [
        f"👋 **¡Hola {first_name}! Soy tu asistente de Antigravity (AGY)** ⚡\n",
        "✨ *Streaming real-time, selector de proyectos y cola concurrente.*\n",
        "🗂️ **Proyectos:**",
        "• 📁 `/project` o `/cd` - Elegir repositorio de trabajo con un toque",
        "• ⚡ `/cd <nombre>` - Cambiar directo a un proyecto\n",
    ]

    if session_state.is_feature_enabled("screen"):
        sections.extend([
            "🖥️ **Control Remoto de PC & Escritorio:**",
            f"• 📸 `/screen` o `/ss` - Captura de pantalla en vivo ({os_label})\n",
        ])

    sections.append("🛠️ **Comandos Activos:**")
    if session_state.is_feature_enabled("obsidian"):
        sections.append("• 🟣 `/obsidian` o `/notas` - Explorar bóveda `.md`, checklists y PDFs (0 tokens)")
        sections.append("• 🎯 `/prompts` - Ver o lanzar prompts técnicos `T01..TXX` en 1 toque")
    if session_state.is_feature_enabled("claude"):
        sections.append("• 🟠 `/claude` - Proyectos y sesiones de Claude Code")
        sections.append("• 🔵 `/agy` - Volver a motor Antigravity (AGY)")

    sections.extend([
        "• 📊 `/usage` o `/tokens` - Ver capacidad y consumo de tokens",
        "• 📂 `/resume` o `/chats` - Historial paginado de chats AGY",
        "• 🗑️ `/delete` o `/borrar` - Eliminar chats individuales",
        "• 🤖 `/model` - Cambiar o sincronizar modelos de IA (`agy models`)",
        "• ⚡ `/effort` - Ajustar velocidad/razonamiento (`low`..`max`)",
        "• 🔄 `/continue` - Usar última conversación",
        "• ➕ `/new` - Nuevo chat independiente",
        "• 📌 `/current` - Ver proyecto, chat y modelo configurados",
        "• ⚙️ `/config` - Activar o quitar módulos del bot",
        "• 💻 `/status` - Estado del servidor, RAM, disco y cola",
        "• 📥 `/queue` o `/cola` - Ver cola de mensajes pendientes",
        "• 🛑 `/cancel` - Cancelar tarea activa de AGY",
        "• 🔌 `/off` o `/power` - Apagar bot de Telegram\n",
        f"📂 **Proyecto activo:** `{cwd_short}`{branch_str}",
        f"📍 **Chat actual:** `{session_state.current_conversation_title}`",
        f"⚡ **Esfuerzo:** `{session_state.current_effort.upper()}` | **Modelo:** {model_disp}",
    ])
    await safe_reply(update.message, "\n".join(sections))


async def cmd_project(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """
    Comando /project, /cd, /workspace o /dir para cambiar el directorio de trabajo (cwd).
    - Sin argumentos: muestra teclado inline paginado con proyectos de /run/media/paulobot/Datos/Projects/*
    - Con argumento: cambia directo por coincidencia exacta/parcial o muestra opciones coincidentes.
    """
    if not is_authorized(update):
        return

    args = context.args
    if not args or len(args) == 0:
        markup, text = build_projects_keyboard(page=0)
        await safe_reply(update.message, text, reply_markup=markup)
        return

    query_str = " ".join(args).strip()
    target_path, matches = resolve_project_Target(query_str)

    if target_path:
        session_state.set_cwd(target_path)
        summary = format_active_cwd_summary()
        slug = get_claude_slug_for_cwd(target_path) if session_state.is_feature_enabled("claude") else None
        keyboard = []
        if slug:
            projects_cl = list_claude_projects()
            cl_idx = next((i for i, p in enumerate(projects_cl) if p["slug"] == slug), None)
            if cl_idx is not None:
                keyboard.append([
                    InlineKeyboardButton(
                        f"🟠 Ver chats de Claude en {os.path.basename(target_path)}",
                        callback_data=f"cl_proj:{cl_idx}:0",
                    )
                ])
        await safe_reply(
            update.message,
            f"✅ **Directorio de trabajo (`cwd`) actualizado:**\n\n{summary}\n\n"
            f"🚀 *Los próximos comandos se ejecutarán directamente dentro de este proyecto.*",
            reply_markup=InlineKeyboardMarkup(keyboard) if keyboard else None,
        )
        return

    if len(matches) > 1:
        projects_all = list_available_projects()
        keyboard = []
        for name, full_path in matches[:10]:
            cb_payload = f"proj_sel:{name}"
            if len(cb_payload.encode("utf-8")) > 64:
                abs_idx = next(
                    (i for i, p in enumerate(projects_all) if p[1] == full_path), 0
                )
                cb_payload = f"proj_idx:{abs_idx}"
            keyboard.append([InlineKeyboardButton(f"📁 {name}", callback_data=cb_payload)])

        keyboard.append([InlineKeyboardButton("🗂️ Ver todos los proyectos", callback_data="proj_page:0")])
        await safe_reply(
            update.message,
            f"🔎 **Se encontraron {len(matches)} proyectos que coinciden con `{query_str}`:**\n"
            "*Toca el que deseas activar:*",
            reply_markup=InlineKeyboardMarkup(keyboard),
        )
        return

    markup, text = build_projects_keyboard(page=0)
    await safe_reply(
        update.message,
        f"❌ No se encontró ningún proyecto ni ruta válida para `{query_str}`.\n\n" + text,
        reply_markup=markup,
    )


async def cmd_usage(update: Update, context: ContextTypes.DEFAULT_TYPE):
    if not is_authorized(update):
        return

    status_msg = await safe_reply(update.message, "🔄 *Sincronizando cuotas de IA (Claude Code & Antigravity)...*")
    markup, text = await build_ai_quotas_dashboard()
    if status_msg:
        await safe_edit(status_msg, text, reply_markup=markup)
    else:
        await safe_reply(update.message, text, reply_markup=markup)


async def cmd_resume(update: Update, context: ContextTypes.DEFAULT_TYPE):
    if not is_authorized(update):
        return
    if session_state.current_engine == "claude":
        slug = get_claude_slug_for_cwd(session_state.current_cwd)
        projects_cl = list_claude_projects()
        cl_idx = next((i for i, p in enumerate(projects_cl) if p["slug"] == slug), None) if slug else None
        if cl_idx is not None:
            markup, text = build_claude_sessions_keyboard(cl_idx, page=0)
        else:
            markup, text = build_claude_projects_keyboard()
        await safe_reply(update.message, text, reply_markup=markup)
        return
    markup, text = build_resume_keyboard(page=0)
    if not markup:
        await safe_reply(update.message, text)
        return
    await safe_reply(update.message, text, reply_markup=markup)


async def cmd_claude(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """Abre el menú exclusivo de proyectos y sesiones de Claude Code."""
    if not is_authorized(update):
        return
    if not session_state.is_feature_enabled("claude"):
        await safe_reply(update.message, "ℹ️ El módulo **Claude Code** está desactivado. Puedes activarlo en `/config`.")
        return
    slug = get_claude_slug_for_cwd(session_state.current_cwd)
    projects_cl = list_claude_projects()
    cl_idx = next((i for i, p in enumerate(projects_cl) if p["slug"] == slug), None) if slug else None
    if cl_idx is not None and len(projects_cl) == 1:
        markup, text = build_claude_sessions_keyboard(cl_idx, page=0)
    else:
        markup, text = build_claude_projects_keyboard()
    await safe_reply(update.message, text, reply_markup=markup)


async def cmd_agy(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """Regresa el motor activo a Antigravity (AGY)."""
    if not is_authorized(update):
        return
    session_state.set_engine("agy")
    chat_guard.touch()
    await safe_reply(
        update.message,
        f"🔵 **Motor activo cambiado a Antigravity (AGY).**\n"
        f"• 💬 **Chat AGY:** `{session_state.current_conversation_title}`\n"
        f"• 📂 **Proyecto (`cwd`):** `{session_state.get_cwd_short_name()}`",
    )


async def cmd_obsidian(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """Abre el explorador nativo de la bóveda Obsidian (0 tokens) o busca texto."""
    if not is_authorized(update):
        return
    if not session_state.is_feature_enabled("obsidian"):
        await safe_reply(update.message, "ℹ️ El módulo **Obsidian & Prompts** está desactivado. Puedes activarlo en `/config`.")
        return
    if context.args and len(context.args) > 0:
        query = " ".join(context.args).strip()
        markup, text = search_vault(query)
        await safe_reply(update.message, text, reply_markup=markup)
        return
    markup, text = build_vault_home_keyboard()
    await safe_reply(update.message, text, reply_markup=markup)


async def cmd_prompts(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """Abre directo los módulos de Prompts Técnicos (T01..TXX) del proyecto actual o de la bóveda."""
    if not is_authorized(update):
        return
    if not session_state.is_feature_enabled("obsidian"):
        await safe_reply(update.message, "ℹ️ El módulo **Obsidian & Prompts** está desactivado. Puedes activarlo en `/config`.")
        return
    projects = list_vault_projects()
    cwd_base = os.path.basename(session_state.current_cwd.rstrip("/")).lower()
    matched_idx = next(
        (i for i, p in enumerate(projects) if str(p["name"]).lower() == cwd_base),
        None,
    )
    if matched_idx is not None:
        markup, text = build_prompt_modules_keyboard(matched_idx)
        if markup:
            await safe_reply(update.message, text, reply_markup=markup)
            return
    markup, text = build_vault_home_keyboard()
    await safe_reply(update.message, text, reply_markup=markup)


async def cmd_delete(update: Update, context: ContextTypes.DEFAULT_TYPE):
    if not is_authorized(update):
        return
    if session_state.current_engine == "claude":
        slug = get_claude_slug_for_cwd(session_state.current_cwd)
        projects_cl = list_claude_projects()
        cl_idx = next((i for i, p in enumerate(projects_cl) if p["slug"] == slug), 0) if slug else 0
        markup, text = build_claude_delete_keyboard(proj_idx=cl_idx, page=0)
        await safe_reply(update.message, text, reply_markup=markup)
        return
    markup, text = build_delete_keyboard(page=0)
    if not markup:
        await safe_reply(update.message, text)
        return
    await safe_reply(update.message, text, reply_markup=markup)


async def cmd_off(update: Update, context: ContextTypes.DEFAULT_TYPE):
    if not is_authorized(update):
        return
    keyboard = [
        [
            InlineKeyboardButton("🔴 Sí, Apagar Bot", callback_data="power:SHUTDOWN"),
            InlineKeyboardButton("❌ Cancelar", callback_data="power:CANCEL"),
        ]
    ]
    reply_markup = InlineKeyboardMarkup(keyboard)
    await safe_reply(
        update.message,
        "🔌 **Control de Energía del Bot**\n\n"
        "¿Deseas detener el servicio del bot de Telegram en tu laptop?\n\n"
        "*(Podrás volver a encenderlo desde el widget de la barra superior o con `telegram-bot-service start`)*.",
        reply_markup=reply_markup,
    )


async def cmd_model(update: Update, context: ContextTypes.DEFAULT_TYPE):
    if not is_authorized(update):
        return

    if context.args and len(context.args) > 0:
        arg = " ".join(context.args).strip()
        if arg.lower() in ("sync", "sincronizar", "refresh", "reload"):
            status_msg = await safe_reply(update.message, "🔄 *Consultando `agy models` en el CLI...*")
            try:
                synced = await sync_models_from_cli()
                markup, text = build_models_keyboard()
                await safe_edit(
                    status_msg,
                    f"✅ **Catálogo sincronizado ({len(synced)} modelos detectados).**\n\n" + text,
                    reply_markup=markup,
                )
            except Exception as e:
                await safe_edit(status_msg, f"❌ Error sincronizando modelos: `{e}`")
            return

        if arg.lower() in ("default", "reset", "defecto"):
            session_state.set_model(None)
            eff_disp, ctx_win = get_effective_model_display()
            await safe_reply(
                update.message,
                f"✅ **Modelo restablecido al Default del CLI:**\n• {eff_disp}\n• Ventana: `{ctx_win:,}` tokens",
            )
            return

        catalog = get_models_catalog()
        matched = next(
            (
                item["id"]
                for item in catalog
                if arg.lower() == item["id"].lower()
                or arg.lower() in item["id"].lower()
                or arg.lower() in item["name"].lower()
            ),
            arg,
        )
        session_state.set_model(matched)
        eff_disp, ctx_win = get_effective_model_display()
        await safe_reply(
            update.message,
            f"✅ **Modelo guardado de forma persistente:**\n• {eff_disp}\n• Ventana de contexto: `{ctx_win:,}` tokens",
        )
        return

    markup, text = build_models_keyboard()
    await safe_reply(update.message, text, reply_markup=markup)


async def cmd_effort(update: Update, context: ContextTypes.DEFAULT_TYPE):
    if not is_authorized(update):
        return

    if context.args and len(context.args) > 0:
        arg = context.args[0].strip().lower()
        if session_state.set_effort(arg):
            await safe_reply(update.message, f"✅ **Nivel de esfuerzo guardado de forma persistente:** `{arg.upper()}`")
            return

    efforts = [
        ("⚡ Low (Mínima latencia / Rápido)", "low"),
        ("⚖️ Medium (Balanceado)", "medium"),
        ("🧠 High (Pensamiento profundo)", "high"),
        ("🚀 X-High (Razonamiento extendido)", "xhigh"),
        ("🔥 Max (Máximo análisis)", "max"),
    ]
    keyboard = []
    for name, code in efforts:
        prefix = "✅ " if code == session_state.current_effort else ""
        keyboard.append([InlineKeyboardButton(f"{prefix}{name}", callback_data=f"eff:{code}")])

    reply_markup = InlineKeyboardMarkup(keyboard)
    await safe_reply(
        update.message,
        f"⚡ **Selecciona el nivel de razonamiento (Esfuerzo/Velocidad)**\n"
        f"*(Actual: `{session_state.current_effort.upper()}` • Persistente tras reinicios)*:",
        reply_markup=reply_markup,
    )


async def cmd_continue(update: Update, context: ContextTypes.DEFAULT_TYPE):
    if not is_authorized(update):
        return
    if session_state.current_engine == "claude":
        session_state.set_claude_session(None, "🔄 Último chat de Claude (-c)", cwd=session_state.current_cwd)
        chat_guard.touch()
        await safe_reply(update.message, "🟠 Modo configurado: **Continuar última conversación de Claude (`-c`)**.")
        return
    session_state.set_conversation(None, "🔄 Continuar último chat activo (-c)")
    chat_guard.touch()
    await safe_reply(update.message, "✅ Modo configurado y guardado: **Continuar última conversación activa**.")


async def cmd_new(update: Update, context: ContextTypes.DEFAULT_TYPE):
    if not is_authorized(update):
        return
    if session_state.current_engine == "claude":
        session_state.set_claude_session("NEW", "➕ Nuevo chat Claude", cwd=session_state.current_cwd)
        chat_guard.touch()
        await safe_reply(update.message, "🟠 Modo configurado: **Nuevo chat limpio de Claude Code**.")
        return
    session_state.set_conversation("NEW", "➕ Nuevo chat independiente")
    chat_guard.touch()
    await safe_reply(update.message, "✅ Modo configurado y guardado: **Nuevo chat limpio**.")


async def cmd_current(update: Update, context: ContextTypes.DEFAULT_TYPE):
    if not is_authorized(update):
        return
    is_claude = session_state.current_engine == "claude"
    engine_str = "🟠 Claude Code" if is_claude else "🔵 Antigravity (AGY)"
    cid_display = (
        (session_state.claude_session_id or "Último (-c)")
        if is_claude
        else (session_state.current_conversation_id or "Último (-c)")
    )
    title_display = session_state.claude_session_title if is_claude else session_state.current_conversation_title
    model_disp, max_ctx = get_effective_model_display()
    cwd_summary = format_active_cwd_summary()

    await safe_reply(
        update.message,
        f"📍 **Configuración Actual de Sesión (Persistente):**\n\n"
        f"⚙️ **Motor activo:** **{engine_str}**\n\n"
        f"🗂️ **Proyecto / Directorio (`cwd`):**\n{cwd_summary}\n\n"
        f"💬 **Chat activo:**\n**{title_display}**\n`ID:` `{cid_display}`\n\n"
        f"🤖 **Modelo AGY:** {model_disp} (`{max_ctx:,}` tokens)\n"
        f"⚡ **Esfuerzo:** `{session_state.current_effort.upper()}`",
    )


async def cmd_status(update: Update, context: ContextTypes.DEFAULT_TYPE):
    if not is_authorized(update):
        return

    disk_target = HOME_DIR if os.path.isdir(HOME_DIR) else os.path.abspath(os.sep)
    total, used, free = shutil.disk_usage(disk_target)
    disk_total = f"{total // (2**30)} GB"
    disk_free = f"{free // (2**30)} GB"
    disk_used = f"{used // (2**30)} GB"

    ram_str = "N/D"
    try:
        import psutil
        vm = psutil.virtual_memory()
        mem_used_gb = round((vm.total - vm.available) / (1024**3), 1)
        mem_total_gb = round(vm.total / (1024**3), 1)
        ram_str = f"{mem_used_gb} / {mem_total_gb} GB"
    except Exception:
        try:
            mem_info = {}
            with open("/proc/meminfo") as f:
                for line in f:
                    parts = line.split(":")
                    if len(parts) == 2:
                        mem_info[parts[0].strip()] = parts[1].strip()
            mem_total_kb = int(mem_info.get("MemTotal", "0 kB").split()[0])
            mem_avail_kb = int(mem_info.get("MemAvailable", "0 kB").split()[0])
            mem_used_gb = round((mem_total_kb - mem_avail_kb) / 1024 / 1024, 1)
            mem_total_gb = round(mem_total_kb / 1024 / 1024, 1)
            ram_str = f"{mem_used_gb} / {mem_total_gb} GB"
        except Exception:
            ram_str = "N/D"

    if execution_manager.is_busy():
        elapsed = format_uptime(execution_manager.get_active_elapsed())
        active_preview = (
            (execution_manager.active_job.prompt[:50] + "...")
            if execution_manager.active_job and len(execution_manager.active_job.prompt) > 50
            else (execution_manager.active_job.prompt if execution_manager.active_job else "Tarea en curso")
        )
        status_agy = f"⏳ Ejecutando (`{elapsed}`) — `{active_preview}`"
    else:
        status_agy = "🟢 Listo / Esperando"

    q_len = len(execution_manager.queue)
    queue_str = f"{q_len} mensaje(s) en espera" if q_len > 0 else "Vacía (0)"

    cwd_short = session_state.get_cwd_short_name()
    branch = get_git_branch(session_state.current_cwd)
    branch_str = f" (`{branch}`)" if branch else ""

    msg = (
        "💻 **Estado del Servidor & AGY**\n\n"
        f"• **Estado AGY:** {status_agy}\n"
        f"• **Cola de Mensajes:** {queue_str}\n"
        f"• **Proyecto (`cwd`):** `{cwd_short}`{branch_str}\n"
        f"• **Chat actual:** {session_state.current_conversation_title}\n"
        f"• **Memoria RAM:** {ram_str}\n"
        f"• **Disco (/home):** {disk_used} usados / {disk_free} libres (Total: {disk_total})"
    )
    await safe_reply(update.message, msg)


async def cmd_queue(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """Muestra la tarea en curso y los mensajes encolados con opciones para gestionarlos."""
    if not is_authorized(update):
        return

    busy = execution_manager.is_busy()
    q_items = list(execution_manager.queue)

    if not busy and not q_items:
        await safe_reply(update.message, "🟢 **Cola vacía:** AGY está libre y no hay mensajes pendientes.")
        return

    lines = ["📥 **Gestor de Procesos y Cola de AGY**\n"]
    keyboard = []

    if busy and execution_manager.active_job:
        elapsed = format_uptime(execution_manager.get_active_elapsed())
        p_text = execution_manager.active_job.prompt.replace("\n", " ")
        preview = p_text[:70] + ("..." if len(p_text) > 70 else "")
        cwd_short = session_state.get_cwd_short_name(execution_manager.active_job.cwd)
        lines.append(f"⚙️ **Ejecutando ahora** (`{elapsed}` • 📂 `{cwd_short}`):\n`{preview}`\n")
        keyboard.append([InlineKeyboardButton("🛑 Cancelar Tarea Actual", callback_data="q_cancel_active")])

    if q_items:
        lines.append(f"⏳ **En espera ({len(q_items)}):**")
        for idx, item in enumerate(q_items, start=1):
            p_text = item.prompt.replace("\n", " ")
            preview = p_text[:50] + ("..." if len(p_text) > 50 else "")
            wait_s = format_uptime(int(time.time() - item.enqueued_at))
            cwd_short = session_state.get_cwd_short_name(item.cwd)
            lines.append(f"`#{idx}` ({wait_s} • 📂 `{cwd_short}`) — `{preview}`")
            keyboard.append([
                InlineKeyboardButton(f"⚡ Forzar #{idx} ya", callback_data=f"q_force:{item.job_id}"),
                InlineKeyboardButton(f"🗑️ Quitar #{idx}", callback_data=f"q_drop:{item.job_id}"),
            ])
        keyboard.append([InlineKeyboardButton("🚫 Cancelar Todo y Vaciar Cola", callback_data="q_cancel_all")])
    else:
        lines.append("ℹ️ *No hay más mensajes en espera.*")

    await safe_reply(
        update.message,
        "\n".join(lines),
        reply_markup=InlineKeyboardMarkup(keyboard) if keyboard else None,
    )


async def cmd_cancel(update: Update, context: ContextTypes.DEFAULT_TYPE):
    if not is_authorized(update):
        return

    q_count = len(execution_manager.queue)
    killed = await execution_manager.cancel_active()

    if killed:
        extra = ""
        markup = None
        if q_count > 0:
            extra = f"\n\n📥 *Tienes {q_count} mensaje(s) en cola que iniciarán a continuación.*"
            markup = InlineKeyboardMarkup([
                [InlineKeyboardButton(f"🗑️ Vaciar también la cola ({q_count})", callback_data="q_cancel_all")]
            ])
        await safe_reply(
            update.message,
            f"🛑 **Tarea activa de AGY y subprocesos cancelados.**{extra}",
            reply_markup=markup,
        )
    elif q_count > 0:
        cleared = await execution_manager.clear_queue()
        await safe_reply(update.message, f"🗑️ **Se vaciaron {cleared} mensaje(s) pendientes de la cola.**")
    else:
        await safe_reply(update.message, "ℹ️ No hay ninguna tarea ejecutándose ni mensajes en cola en este momento.")


async def cmd_screen(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """Toma una captura de pantalla del escritorio de la PC y la envía por Telegram."""
    if not is_authorized(update):
        return
    if not session_state.is_feature_enabled("screen"):
        await safe_reply(update.message, "ℹ️ El módulo **Captura de Pantalla** está desactivado. Puedes activarlo en `/config`.")
        return

    backend_lbl = "Windows" if IS_WINDOWS else "Wayland / grim"
    status_msg = await safe_reply(update.message, f"📸 *Capturando pantalla de tu PC ({backend_lbl})...*")
    ok, result = await capture_wayland_screenshot()

    if not ok:
        await safe_edit(status_msg, f"❌ **No se pudo tomar la captura:**\n`{result}`")
        return

    try:
        caption = f"🖥️ **Captura de Escritorio** • `{time.strftime('%Y-%m-%d %H:%M:%S')}`"
        with open(result, "rb") as photo_file:
            await update.message.reply_photo(
                photo=photo_file,
                caption=caption,
                parse_mode="Markdown",
                reply_markup=build_screenshot_keyboard(),
            )
        if status_msg:
            await status_msg.delete()
    except Exception as e:
        await safe_edit(status_msg, f"❌ **Error enviando captura a Telegram:** `{e}`")
    finally:
        if os.path.exists(result):
            try:
                os.remove(result)
            except Exception:
                pass


