import os
import time
import asyncio
from telegram import Update, InlineKeyboardButton, InlineKeyboardMarkup
from telegram.ext import ContextTypes
from core.config import HOME_DIR, PROJECTS_BASE_DIR
from core.state import session_state
from core.utils import is_authorized, safe_edit, safe_reply, format_uptime, get_git_branch
from core.chats_db import (
    get_conversation_title,
    delete_single_chat,
    build_resume_keyboard,
    build_delete_keyboard,
)
from core.projects import (
    build_projects_keyboard,
    format_active_cwd_summary,
    list_available_projects,
)
from core.models import (
    build_models_keyboard,
    get_effective_model_display,
    get_models_catalog,
    sync_models_from_cli,
)
from core.desktop_tools import (
    capture_wayland_screenshot,
    build_screenshot_keyboard,
)
from core.executor import execution_manager
from core.chat_guard import chat_guard


async def shutdown_bot_service():
    await asyncio.sleep(1.0)
    proc = await asyncio.create_subprocess_exec("systemctl", "--user", "stop", "telegram-bot.service")
    await proc.wait()


async def handle_callback_query(update: Update, context: ContextTypes.DEFAULT_TYPE):
    query = update.callback_query
    if not query or not is_authorized(update):
        return

    await query.answer()
    data = query.data

    if data == "res_noop":
        return

    # =========================================================
    # 🧭 CALLBACKS DE GUARDIA DE CHAT (confirmar chat tras inactividad)
    # =========================================================
    if data.startswith("cg_pick:"):
        _, hold_id, page_str = data.split(":", 2)
        view = chat_guard.build_picker_view(hold_id, page=int(page_str))
        if view:
            markup, text = view
            await safe_edit(query.message, text, reply_markup=markup)
        else:
            await safe_edit(query.message, "⌛ Esta pregunta expiró o no hay chats guardados.")
        return

    if data.startswith("cg_back:"):
        hold_id = data.split(":", 1)[1]
        view = chat_guard.build_prompt_view(hold_id)
        if view:
            markup, text = view
            await safe_edit(query.message, text, reply_markup=markup)
        else:
            await safe_edit(query.message, "⌛ Esta pregunta expiró. Reenvía el mensaje.")
        return

    if data.startswith("cg_sel:"):
        _, hold_id, idx_str = data.split(":", 2)
        result = await chat_guard.resolve("sel", hold_id, execution_manager.submit, chat_idx=int(idx_str))
        await safe_edit(query.message, result or "⌛ Esta pregunta expiró. Reenvía el mensaje.")
        return

    if data.startswith(("cg_cont:", "cg_new:", "cg_drop:")):
        action_key, hold_id = data.split(":", 1)
        action = {"cg_cont": "cont", "cg_new": "new", "cg_drop": "drop"}[action_key]
        result = await chat_guard.resolve(action, hold_id, execution_manager.submit)
        await safe_edit(query.message, result or "⌛ Esta pregunta expiró. Reenvía el mensaje.")
        return

    # =========================================================
    # 🗂️ CALLBACKS DE SELECTOR DE PROYECTO / DIRECTORIO (cwd)
    # =========================================================
    if data.startswith("proj_page:"):
        page = int(data[10:])
        markup, text = build_projects_keyboard(page=page)
        await safe_edit(query.message, text, reply_markup=markup)
        return

    if data == "proj_home":
        session_state.set_cwd(HOME_DIR)
        summary = format_active_cwd_summary()
        await safe_edit(
            query.message,
            f"✅ **Directorio de trabajo (`cwd`) cambiado a Home:**\n\n{summary}\n\n"
            "🚀 *Los próximos comandos de AGY se ejecutarán aquí.*",
        )
        return

    if data.startswith("proj_sel:"):
        folder_name = data[9:]
        target_path = os.path.join(PROJECTS_BASE_DIR, folder_name)
        if session_state.set_cwd(target_path):
            summary = format_active_cwd_summary()
            await safe_edit(
                query.message,
                f"✅ **Proyecto activado:**\n\n{summary}\n\n"
                "🚀 *Los próximos comandos de AGY se ejecutarán directamente dentro de este repositorio.*",
            )
        else:
            await safe_edit(query.message, f"❌ La carpeta `{target_path}` ya no existe.")
        return

    if data.startswith("proj_idx:"):
        idx = int(data[9:])
        projects = list_available_projects()
        if 0 <= idx < len(projects):
            _, target_path, _ = projects[idx]
            if session_state.set_cwd(target_path):
                summary = format_active_cwd_summary()
                await safe_edit(
                    query.message,
                    f"✅ **Proyecto activado:**\n\n{summary}\n\n"
                    "🚀 *Los próximos comandos de AGY se ejecutarán directamente dentro de este repositorio.*",
                )
                return
        await safe_edit(query.message, "❌ No se pudo encontrar el proyecto seleccionado.")
        return

    # =========================================================
    # 💬 CALLBACKS DE HISTORIAL Y BORRADO DE CHATS
    # =========================================================
    if data.startswith("res_page:"):
        page = int(data[9:])
        markup, text = build_resume_keyboard(page=page)
        if markup:
            await safe_edit(query.message, text, reply_markup=markup)
        return

    if data.startswith("del_menu:") or data.startswith("del_page:"):
        page = int(data[9:])
        markup, text = build_delete_keyboard(page=page)
        if markup:
            await safe_edit(query.message, text, reply_markup=markup)
        return

    if data.startswith("del_ask:"):
        conv_id = data[8:]
        title = get_conversation_title(conv_id)
        keyboard = [
            [
                InlineKeyboardButton("🔴 Sí, Eliminar Definitivamente", callback_data=f"del_confirm:{conv_id}"),
                InlineKeyboardButton("❌ Cancelar", callback_data="del_menu:0"),
            ]
        ]
        await safe_edit(
            query.message,
            f"⚠️ **¿Estás seguro de eliminar esta conversación?**\n\n"
            f"💬 **Título:** {title}\n"
            f"`ID:` `{conv_id}`\n\n"
            f"*Esta acción borrará el historial y sus archivos del disco.*",
            reply_markup=InlineKeyboardMarkup(keyboard),
        )
        return

    if data.startswith("del_confirm:"):
        conv_id = data[12:]
        title = get_conversation_title(conv_id)
        delete_single_chat(conv_id)
        markup, text = build_delete_keyboard(page=0)
        if markup:
            await safe_edit(
                query.message,
                f"✅ **Conversación eliminada con éxito:**\n`{title}`\n\n" + text,
                reply_markup=markup,
            )
        else:
            await safe_edit(query.message, f"✅ Conversación '{title}' eliminada. Ya no quedan más chats.")
        return

    if data.startswith("res:"):
        target = data[4:]
        if target == "LATEST":
            session_state.set_conversation(None, "🔄 Continuar último chat activo (-c)")
            chat_guard.touch()
            await safe_edit(query.message, "✅ **Configurado y guardado:** Ahora enviarás mensajes a la **última conversación activa**.")
        elif target == "NEW":
            session_state.set_conversation("NEW", "➕ Nuevo chat independiente")
            chat_guard.touch()
            await safe_edit(query.message, "✅ **Configurado y guardado:** El siguiente mensaje creará un **nuevo chat limpio**.")
        else:
            title = get_conversation_title(target)
            session_state.set_conversation(target, f"💬 {title}")
            chat_guard.touch()
            await safe_edit(
                query.message,
                f"✅ **Chat seleccionado y guardado:**\n**{title}**\n\n*(ID: `{target}`)*\n\nCualquier mensaje que envíes continuará en este hilo.",
            )
        return

    # =========================================================
    # 🤖 CALLBACKS DE MODELO, ESFUERZO Y ENERGÍA
    # =========================================================
    if data == "mod_sync":
        await safe_edit(query.message, "🔄 *Consultando `agy models` para sincronizar el catálogo...*")
        try:
            synced = await sync_models_from_cli()
            markup, text = build_models_keyboard()
            await safe_edit(
                query.message,
                f"✅ **Catálogo actualizado ({len(synced)} modelos sincronizados con AGY CLI).**\n\n" + text,
                reply_markup=markup,
            )
        except Exception as e:
            await safe_edit(query.message, f"❌ Error sincronizando modelos desde el CLI: `{e}`")
        return

    if data.startswith("mod:"):
        target = data[4:]
        if target == "DEFAULT":
            session_state.set_model(None)
        else:
            session_state.set_model(target)
        eff_disp, ctx_win = get_effective_model_display()
        await safe_edit(
            query.message,
            f"✅ **Modelo configurado y guardado:**\n\n"
            f"• 🤖 **Modelo activo:** {eff_disp}\n"
            f"• ⚡ **Esfuerzo sincronizado:** `{session_state.current_effort.upper()}`\n"
            f"• 🪟 **Ventana de contexto:** `{ctx_win:,}` tokens",
        )
        return

    if data.startswith("eff:"):
        target = data[4:]
        session_state.set_effort(target)
        eff_disp, _ = get_effective_model_display()
        await safe_edit(
            query.message,
            f"✅ **Nivel de razonamiento guardado:** `{target.upper()}`\n"
            f"• 🤖 **Modelo activo:** {eff_disp}",
        )
        return

    if data.startswith("power:"):
        action = data[6:]
        if action == "CANCEL":
            await safe_edit(query.message, "✅ **Operación cancelada:** El bot sigue encendido y activo.")
        elif action == "SHUTDOWN":
            await safe_edit(query.message, "👋 **Apagando bot de Telegram...**\n\nEl servicio se ha detenido. ¡Nos vemos!")
            asyncio.create_task(shutdown_bot_service())
        return

    # =========================================================
    # ⚙️ CALLBACKS DE COLA Y CONTROL DE PROCESOS AGY
    # =========================================================
    if data == "q_cancel_active":
        killed = await execution_manager.cancel_active()
        if not killed:
            await query.answer("No había ningún proceso activo.", show_alert=False)
        return

    if data == "q_cancel_all":
        cleared = await execution_manager.clear_queue()
        killed = await execution_manager.cancel_active()
        await safe_edit(
            query.message,
            f"🛑 **Ejecución detenida y cola vaciada.**\n"
            f"• Proceso activo cancelado: `{'Sí' if killed else 'No'}`\n"
            f"• Mensajes en cola descartados: `{cleared}`",
        )
        return

    if data.startswith("q_drop:"):
        job_id = data[7:]
        removed = await execution_manager.drop_queued_job(job_id)
        if removed:
            preview = removed.prompt[:80] + ("..." if len(removed.prompt) > 80 else "")
            await safe_edit(
                query.message,
                f"🗑️ **Mensaje retirado de la cola:**\n`{preview}`",
            )
        else:
            await safe_edit(query.message, "ℹ️ Este mensaje ya había iniciado su ejecución o ya no estaba en la cola.")
        return

    if data.startswith("q_force:"):
        job_id = data[8:]
        forced = await execution_manager.force_job_now(job_id)
        if forced:
            await safe_edit(
                query.message,
                "⚡ **Interrumpiendo tarea anterior...**\n"
                "🚀 *Tu mensaje ha pasado al frente y se ejecutará de inmediato.*",
            )
        else:
            await safe_edit(query.message, "ℹ️ El mensaje ya se encuentra en ejecución o no estaba en la cola.")
        return

    # =========================================================
    # 📸 CALLBACKS DE CAPTURA DE PANTALLA (/screen o /ss)
    # =========================================================
    if data == "screen:delete":
        try:
            await query.message.delete()
        except Exception:
            pass
        return

    if data == "screen:capture":
        ok, result = await capture_wayland_screenshot()
        if not ok:
            await safe_reply(query.message, f"❌ **Error tomando captura:** `{result}`")
            return
        try:
            caption = f"🖥️ **Captura de Escritorio** • `{time.strftime('%Y-%m-%d %H:%M:%S')}`"
            with open(result, "rb") as photo_file:
                await query.message.reply_photo(
                    photo=photo_file,
                    caption=caption,
                    parse_mode="Markdown",
                    reply_markup=build_screenshot_keyboard(),
                )
        finally:
            if os.path.exists(result):
                try:
                    os.remove(result)
                except Exception:
                    pass
        return

