import os
import shutil
import time
import tempfile
from telegram import Update, InlineKeyboardButton, InlineKeyboardMarkup
from telegram.ext import ContextTypes
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
from core.executor import execution_manager
from core.message_buffer import long_message_buffer
from core.chat_guard import chat_guard


async def cmd_start(update: Update, context: ContextTypes.DEFAULT_TYPE):
    if not is_authorized(update):
        return
    model_disp, _ = get_effective_model_display()
    cwd_short = session_state.get_cwd_short_name()
    branch = get_git_branch(session_state.current_cwd)
    branch_str = f" (`{branch}`)" if branch else ""

    msg = (
        "👋 **¡Hola Paulo! Soy tu asistente de Antigravity (AGY)** ⚡\n\n"
        "✨ *Streaming real-time, selector de proyectos y cola concurrente.*\n\n"
        "🗂️ **Proyectos:**\n"
        "• 📁 `/project` o `/cd` - Elegir repositorio de trabajo con un toque\n"
        "• ⚡ `/cd <nombre>` - Cambiar directo (ej. `/cd wicode-back`, `/cd ~`)\n\n"
        "🖥️ **Control Remoto de PC & Escritorio:**\n"
        "• 📸 `/screen` o `/ss` - Captura de pantalla en vivo de tu laptop (Wayland)\n\n"
        "🛠️ **Comandos de AGY:**\n"
        "• 📊 `/usage` o `/tokens` - Ver capacidad y consumo de tokens\n"
        "• 📂 `/resume` o `/chats` - Historial paginado de chats\n"
        "• 🗑️ `/delete` o `/borrar` - Eliminar chats individuales\n"
        "• 🤖 `/model` - Cambiar o sincronizar modelos de IA (`agy models`)\n"
        "• ⚡ `/effort` - Ajustar velocidad/razonamiento (`low`..`max`)\n"
        "• 🔄 `/continue` - Usar última conversación\n"
        "• ➕ `/new` - Nuevo chat independiente\n"
        "• 📌 `/current` - Ver proyecto, chat y modelo configurados\n"
        "• 💻 `/status` - Estado del servidor, RAM, disco y cola\n"
        "• 📥 `/queue` o `/cola` - Ver cola de mensajes pendientes\n"
        "• 🛑 `/cancel` - Cancelar tarea activa de AGY\n"
        "• 🔌 `/off` o `/power` - Apagar bot de Telegram\n\n"
        f"📂 **Proyecto activo:** `{cwd_short}`{branch_str}\n"
        f"📍 **Chat actual:** `{session_state.current_conversation_title}`\n"
        f"⚡ **Esfuerzo:** `{session_state.current_effort.upper()}` | **Modelo:** {model_disp}"
    )
    await safe_reply(update.message, msg)


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
        await safe_reply(
            update.message,
            f"✅ **Directorio de trabajo (`cwd`) actualizado:**\n\n{summary}\n\n"
            f"🚀 *Los próximos comandos de AGY se ejecutarán directamente dentro de este proyecto.*",
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
    markup, text = build_resume_keyboard(page=0)
    if not markup:
        await safe_reply(update.message, text)
        return
    await safe_reply(update.message, text, reply_markup=markup)


async def cmd_delete(update: Update, context: ContextTypes.DEFAULT_TYPE):
    if not is_authorized(update):
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
    session_state.set_conversation(None, "🔄 Continuar último chat activo (-c)")
    chat_guard.touch()
    await safe_reply(update.message, "✅ Modo configurado y guardado: **Continuar última conversación activa**.")


async def cmd_new(update: Update, context: ContextTypes.DEFAULT_TYPE):
    if not is_authorized(update):
        return
    session_state.set_conversation("NEW", "➕ Nuevo chat independiente")
    chat_guard.touch()
    await safe_reply(update.message, "✅ Modo configurado y guardado: **Nuevo chat limpio**.")


async def cmd_current(update: Update, context: ContextTypes.DEFAULT_TYPE):
    if not is_authorized(update):
        return
    cid_display = session_state.current_conversation_id if session_state.current_conversation_id else "Último (-c)"
    model_disp, max_ctx = get_effective_model_display()
    cwd_summary = format_active_cwd_summary()

    await safe_reply(
        update.message,
        f"📍 **Configuración Actual de Sesión (Persistente):**\n\n"
        f"🗂️ **Proyecto / Directorio (`cwd`):**\n{cwd_summary}\n\n"
        f"💬 **Chat activo:**\n**{session_state.current_conversation_title}**\n`ID:` `{cid_display}`\n\n"
        f"🤖 **Modelo:** {model_disp} (`{max_ctx:,}` tokens)\n"
        f"⚡ **Esfuerzo:** `{session_state.current_effort.upper()}`\n"
        f"💾 **Estado guardado en:** `~/.gemini/antigravity-cli/telegram_bot_state.json`",
    )


async def cmd_status(update: Update, context: ContextTypes.DEFAULT_TYPE):
    if not is_authorized(update):
        return

    total, used, free = shutil.disk_usage("/home/paulobot")
    disk_total = f"{total // (2**30)} GB"
    disk_free = f"{free // (2**30)} GB"
    disk_used = f"{used // (2**30)} GB"

    mem_info = {}
    try:
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
    """Toma una captura de pantalla del escritorio de la PC con grim y la envía por Telegram."""
    if not is_authorized(update):
        return

    status_msg = await safe_reply(update.message, "📸 *Capturando pantalla de tu PC (Wayland / grim)...*")
    ok, result = await capture_wayland_screenshot()

    if not ok:
        await safe_edit(status_msg, f"❌ **No se pudo tomar la captura con `grim`:**\n`{result}`")
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


async def handle_message(update: Update, context: ContextTypes.DEFAULT_TYPE):
    if not is_authorized(update) or not update.message:
        return

    text_prompt = (update.message.text or "").strip()
    caption_prompt = (update.message.caption or "").strip()
    prompt = text_prompt or caption_prompt
    temp_files = []

    # Si el mensaje incluye foto
    if update.message.photo:
        try:
            largest_photo = update.message.photo[-1]
            file = await largest_photo.get_file()
            tmp_img = os.path.join(tempfile.gettempdir(), f"telegram_photo_{largest_photo.file_unique_id}.jpg")
            await file.download_to_drive(tmp_img)
            temp_files.append(tmp_img)

            if prompt:
                prompt = f"[Foto adjunta guardada en: {tmp_img}]\n\n{prompt}"
            else:
                prompt = f"[Foto adjunta guardada en: {tmp_img}]\nPor favor analiza esta imagen."
        except Exception as e:
            await safe_reply(update.message, f"⚠️ Error descargando la imagen: `{e}`")
            return

    if not prompt:
        return

    # Mensajes > LONG_MESSAGE_THRESHOLD: esperar fragmentos y unirlos.
    # Todo pasa por chat_guard, que pregunta el chat si hubo inactividad.
    buffered = await long_message_buffer.handle(
        update.message.chat_id, prompt, update.message, chat_guard_submit, temp_files=temp_files
    )
    if buffered:
        return

    await chat_guard_submit(prompt, update.message, temp_files=temp_files)


async def chat_guard_submit(prompt: str, message_obj, temp_files=None):
    await chat_guard.handle(prompt, message_obj, execution_manager.submit, temp_files=temp_files)

