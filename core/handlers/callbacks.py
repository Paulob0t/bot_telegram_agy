"""
Enrutador modular de CallbackQuery (botones inline) organizado por dominio:
- Claude Code (`cl_*`)
- ChatGuard (`cg_*`)
- Proyectos / cwd (`proj_*`)
- Historial y borrado AGY (`res_*`, `del_*`)
- Modelos, Esfuerzo y Uso (`mod*`, `eff:*`, `usage_sync`, `power:*`)
- Cola de procesos y Capturas (`q_*`, `screen:*`)
"""
import asyncio
import os
import time
from telegram import InlineKeyboardButton, InlineKeyboardMarkup, Update
from telegram.ext import ContextTypes
from core.chat_guard import chat_guard
from core.chats_db import (
    build_delete_keyboard,
    build_resume_keyboard,
    delete_single_chat,
    get_conversation_title,
)
from core.claude_chats import (
    build_claude_delete_keyboard,
    build_claude_projects_keyboard,
    build_claude_sessions_keyboard,
    delete_claude_session,
    find_session_by_prefix,
    get_claude_slug_for_cwd,
    list_claude_projects,
)
from core.config import HOME_DIR, IS_WINDOWS, PROJECTS_BASE_DIR
from core.desktop_tools import build_screenshot_keyboard, capture_wayland_screenshot
from core.executor import execution_manager
from core.models import (
    build_ai_quotas_dashboard,
    build_models_keyboard,
    get_effective_model_display,
    sync_models_from_cli,
)
from core.projects import (
    build_projects_keyboard,
    format_active_cwd_summary,
    list_available_projects,
)
from core.obsidian import (
    PROJECT_CODE_MAP,
    build_module_prompts_keyboard,
    build_note_checklist_view,
    build_note_reader_view,
    build_notes_list_keyboard,
    build_pdfs_list_keyboard,
    build_prompt_launch_confirm,
    build_prompt_modules_keyboard,
    build_prompt_reader_view,
    build_vault_home_keyboard,
    build_vault_project_keyboard,
    build_wicode_generator_prompt,
    list_module_prompts,
    list_project_notes,
    list_project_pdfs,
    list_prompt_modules,
    list_vault_projects,
    pop_pending_pdf,
    toggle_checklist_line,
)
from core.handlers.commands import build_config_view, sync_telegram_commands_menu
from core.state import session_state
from core.utils import is_authorized, safe_edit, safe_reply


async def _shutdown_bot_service():
    await asyncio.sleep(1.0)
    if IS_WINDOWS:
        os._exit(0)
    proc = await asyncio.create_subprocess_exec("systemctl", "--user", "stop", "telegram-bot.service")
    await proc.wait()


async def _handle_claude_callbacks(query, data: str) -> bool:
    if data == "cl_home":
        markup, text = build_claude_projects_keyboard()
        await safe_edit(query.message, text, reply_markup=markup)
        return True

    if data == "cl_engine:agy":
        session_state.set_engine("agy")
        chat_guard.touch()
        await safe_edit(
            query.message,
            f"🔵 **Motor cambiado a Antigravity (AGY).**\n"
            f"• 💬 **Chat AGY:** `{session_state.current_conversation_title}`\n"
            f"• 📂 **Proyecto (`cwd`):** `{session_state.get_cwd_short_name()}`",
        )
        return True

    if data.startswith("cl_proj:"):
        _, proj_idx_s, page_s = data.split(":", 2)
        markup, text = build_claude_sessions_keyboard(int(proj_idx_s), page=int(page_s))
        await safe_edit(query.message, text, reply_markup=markup)
        return True

    if data.startswith("cl_sel:"):
        _, proj_idx_s, short_id = data.split(":", 2)
        projects = list_claude_projects()
        pidx = int(proj_idx_s)
        if 0 <= pidx < len(projects):
            proj = projects[pidx]
            sess = find_session_by_prefix(str(proj["slug"]), short_id)
            if sess:
                session_state.set_claude_session(sess["session_id"], sess["title"], cwd=str(proj["cwd"]))
                chat_guard.touch()
                await safe_edit(
                    query.message,
                    f"🟠 **Sesión de Claude Code activada:**\n\n"
                    f"• 💬 **Chat:** `{sess['title']}`\n"
                    f"• 🆔 **ID:** `{sess['session_id']}`\n"
                    f"• 📂 **Proyecto (`cwd`):** `{proj['name']}`\n\n"
                    f"🚀 *Cualquier mensaje que envíes ahora continuará este hilo con Claude (`claude -r`). Para regresar a AGY usa `/agy`.*",
                )
                return True
        await safe_edit(query.message, "❌ No se encontró esa sesión de Claude.")
        return True

    if data.startswith("cl_cont:"):
        pidx = int(data.split(":", 1)[1])
        projects = list_claude_projects()
        if 0 <= pidx < len(projects):
            proj = projects[pidx]
            session_state.set_claude_session(None, f"🔄 Último chat en {proj['name']} (-c)", cwd=str(proj["cwd"]))
            chat_guard.touch()
            await safe_edit(
                query.message,
                f"🟠 **Claude Code configurado en modo `-c` (Último activo):**\n"
                f"• 📂 **Proyecto (`cwd`):** `{proj['name']}`\n\n"
                f"🚀 *Tus próximos mensajes continuarán la última sesión de Claude en este proyecto.*",
            )
            return True

    if data.startswith("cl_new:"):
        pidx = int(data.split(":", 1)[1])
        projects = list_claude_projects()
        if 0 <= pidx < len(projects):
            proj = projects[pidx]
            session_state.set_claude_session("NEW", f"➕ Nuevo chat en {proj['name']}", cwd=str(proj["cwd"]))
            chat_guard.touch()
            await safe_edit(
                query.message,
                f"🟠 **Nuevo chat de Claude Code preparado:**\n"
                f"• 📂 **Proyecto (`cwd`):** `{proj['name']}`\n\n"
                f"🚀 *El siguiente mensaje abrirá una conversación nueva de Claude en este proyecto.*",
            )
            return True

    if data.startswith("cl_del_page:"):
        _, proj_idx_s, page_s = data.split(":", 2)
        markup, text = build_claude_delete_keyboard(int(proj_idx_s), page=int(page_s))
        await safe_edit(query.message, text, reply_markup=markup)
        return True

    if data.startswith("cl_del_ask:"):
        _, proj_idx_s, short_id = data.split(":", 2)
        pidx = int(proj_idx_s)
        projects = list_claude_projects()
        if 0 <= pidx < len(projects):
            proj = projects[pidx]
            sess = find_session_by_prefix(str(proj["slug"]), short_id)
            if sess:
                keyboard = InlineKeyboardMarkup([
                    [
                        InlineKeyboardButton("🔴 Sí, Borrar .jsonl", callback_data=f"cl_del_ok:{pidx}:{short_id}"),
                        InlineKeyboardButton("❌ Cancelar", callback_data=f"cl_del_page:{pidx}:0"),
                    ]
                ])
                await safe_edit(
                    query.message,
                    f"⚠️ **¿Eliminar sesión de Claude en `{proj['name']}`?**\n\n"
                    f"💬 **Título:** `{sess['title']}`\n"
                    f"🆔 **ID:** `{sess['session_id']}`",
                    reply_markup=keyboard,
                )
                return True
        await safe_edit(query.message, "❌ Sesión de Claude no encontrada.")
        return True

    if data.startswith("cl_del_ok:"):
        _, proj_idx_s, short_id = data.split(":", 2)
        pidx = int(proj_idx_s)
        projects = list_claude_projects()
        if 0 <= pidx < len(projects):
            proj = projects[pidx]
            slug = str(proj["slug"])
            sess = find_session_by_prefix(slug, short_id)
            if sess and delete_claude_session(slug, sess["session_id"]):
                markup, text = build_claude_delete_keyboard(pidx, page=0)
                await safe_edit(
                    query.message,
                    f"✅ **Sesión de Claude eliminada:** `{sess['title']}`\n\n{text}",
                    reply_markup=markup,
                )
                return True
        await safe_edit(query.message, "❌ No se pudo eliminar la sesión de Claude.")
        return True

    return False


async def _handle_chat_guard_callbacks(query, data: str) -> bool:
    if data.startswith("cg_pick:"):
        _, hold_id, page_str = data.split(":", 2)
        view = chat_guard.build_picker_view(hold_id, page=int(page_str))
        if view:
            markup, text = view
            await safe_edit(query.message, text, reply_markup=markup)
        else:
            await safe_edit(query.message, "⌛ Esta pregunta expiró o no hay chats guardados.")
        return True

    if data.startswith("cg_back:"):
        hold_id = data.split(":", 1)[1]
        view = chat_guard.build_prompt_view(hold_id)
        if view:
            markup, text = view
            await safe_edit(query.message, text, reply_markup=markup)
        else:
            await safe_edit(query.message, "⌛ Esta pregunta expiró. Reenvía el mensaje.")
        return True

    if data.startswith("cg_sel:"):
        _, hold_id, idx_str = data.split(":", 2)
        result = await chat_guard.resolve("sel", hold_id, execution_manager.submit, chat_idx=int(idx_str))
        await safe_edit(query.message, result or "⌛ Esta pregunta expiró. Reenvía el mensaje.")
        return True

    if data.startswith(("cg_cont:", "cg_new:", "cg_drop:")):
        action_key, hold_id = data.split(":", 1)
        action = {"cg_cont": "cont", "cg_new": "new", "cg_drop": "drop"}[action_key]
        result = await chat_guard.resolve(action, hold_id, execution_manager.submit)
        await safe_edit(query.message, result or "⌛ Esta pregunta expiró. Reenvía el mensaje.")
        return True

    return False


def _build_project_claude_shortcut(target_path: str, label_name: str):
    if not session_state.is_feature_enabled("claude"):
        return None
    slug = get_claude_slug_for_cwd(target_path)
    if not slug:
        return None
    projects_cl = list_claude_projects()
    cl_idx = next((i for i, p in enumerate(projects_cl) if p["slug"] == slug), None)
    if cl_idx is None:
        return None
    return InlineKeyboardMarkup([
        [InlineKeyboardButton(f"🟠 Ver chats de Claude en {label_name}", callback_data=f"cl_proj:{cl_idx}:0")]
    ])


async def _handle_project_callbacks(query, data: str) -> bool:
    if data.startswith("proj_page:"):
        page = int(data[10:])
        markup, text = build_projects_keyboard(page=page)
        await safe_edit(query.message, text, reply_markup=markup)
        return True

    if data == "proj_home":
        session_state.set_cwd(HOME_DIR)
        summary = format_active_cwd_summary()
        await safe_edit(
            query.message,
            f"✅ **Directorio de trabajo (`cwd`) cambiado a Home:**\n\n{summary}\n\n"
            "🚀 *Los próximos comandos se ejecutarán aquí.*",
        )
        return True

    if data.startswith("proj_sel:"):
        folder_name = data[9:]
        target_path = os.path.join(PROJECTS_BASE_DIR, folder_name)
        if session_state.set_cwd(target_path):
            summary = format_active_cwd_summary()
            await safe_edit(
                query.message,
                f"✅ **Proyecto activado:**\n\n{summary}\n\n"
                "🚀 *Los próximos comandos se ejecutarán directamente dentro de este repositorio.*",
                reply_markup=_build_project_claude_shortcut(target_path, folder_name),
            )
        else:
            await safe_edit(query.message, f"❌ La carpeta `{target_path}` ya no existe.")
        return True

    if data.startswith("proj_idx:"):
        idx = int(data[9:])
        projects = list_available_projects()
        if 0 <= idx < len(projects):
            name_p, target_path, _ = projects[idx]
            if session_state.set_cwd(target_path):
                summary = format_active_cwd_summary()
                await safe_edit(
                    query.message,
                    f"✅ **Proyecto activado:**\n\n{summary}\n\n"
                    "🚀 *Los próximos comandos se ejecutarán directamente dentro de este repositorio.*",
                    reply_markup=_build_project_claude_shortcut(target_path, name_p),
                )
                return True
        await safe_edit(query.message, "❌ No se pudo encontrar el proyecto seleccionado.")
        return True

    return False


async def _handle_agy_chats_callbacks(query, data: str) -> bool:
    if data.startswith("res_page:"):
        page = int(data[9:])
        markup, text = build_resume_keyboard(page=page)
        if markup:
            await safe_edit(query.message, text, reply_markup=markup)
        return True

    if data.startswith(("del_menu:", "del_page:")):
        page = int(data[9:])
        markup, text = build_delete_keyboard(page=page)
        if markup:
            await safe_edit(query.message, text, reply_markup=markup)
        return True

    if data.startswith("del_ask:"):
        conv_id = data[8:]
        title = get_conversation_title(conv_id)
        keyboard = InlineKeyboardMarkup([
            [
                InlineKeyboardButton("🔴 Sí, Eliminar Definitivamente", callback_data=f"del_confirm:{conv_id}"),
                InlineKeyboardButton("❌ Cancelar", callback_data="del_menu:0"),
            ]
        ])
        await safe_edit(
            query.message,
            f"⚠️ **¿Estás seguro de eliminar esta conversación?**\n\n"
            f"💬 **Título:** {title}\n"
            f"`ID:` `{conv_id}`\n\n"
            f"*Esta acción borrará el historial y sus archivos del disco.*",
            reply_markup=keyboard,
        )
        return True

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
        return True

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
        return True

    return False


async def _handle_model_and_system_callbacks(query, data: str) -> bool:
    if data.startswith("cfg_tog:"):
        key = data.split(":", 1)[1]
        session_state.toggle_feature(key)
        await sync_telegram_commands_menu(query.get_bot())
        markup, text = build_config_view()
        await safe_edit(query.message, text, reply_markup=markup)
        return True

    if data == "usage_sync":
        await safe_edit(query.message, "🔄 *Sincronizando cuotas de IA (Claude Code & Antigravity)...*")
        markup, text = await build_ai_quotas_dashboard()
        await safe_edit(query.message, text, reply_markup=markup)
        return True

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
        return True

    if data.startswith("mod:"):
        target = data[4:]
        session_state.set_model(None if target == "DEFAULT" else target)
        eff_disp, ctx_win = get_effective_model_display()
        await safe_edit(
            query.message,
            f"✅ **Modelo configurado y guardado:**\n\n"
            f"• 🤖 **Modelo activo:** {eff_disp}\n"
            f"• ⚡ **Esfuerzo sincronizado:** `{session_state.current_effort.upper()}`\n"
            f"• 🪟 **Ventana de contexto:** `{ctx_win:,}` tokens",
        )
        return True

    if data.startswith("eff:"):
        target = data[4:]
        session_state.set_effort(target)
        eff_disp, _ = get_effective_model_display()
        await safe_edit(
            query.message,
            f"✅ **Nivel de razonamiento guardado:** `{target.upper()}`\n"
            f"• 🤖 **Modelo activo:** {eff_disp}",
        )
        return True

    if data.startswith("power:"):
        action = data[6:]
        if action == "CANCEL":
            await safe_edit(query.message, "✅ **Operación cancelada:** El bot sigue encendido y activo.")
        elif action == "SHUTDOWN":
            await safe_edit(query.message, "👋 **Apagando bot de Telegram...**\n\nEl servicio se ha detenido. ¡Nos vemos!")
            asyncio.create_task(_shutdown_bot_service())
        return True

    return False


async def _handle_queue_and_screen_callbacks(query, data: str) -> bool:
    if data == "q_cancel_active":
        killed = await execution_manager.cancel_active()
        if not killed:
            await query.answer("No había ningún proceso activo.", show_alert=False)
        return True

    if data == "q_cancel_all":
        cleared = await execution_manager.clear_queue()
        killed = await execution_manager.cancel_active()
        await safe_edit(
            query.message,
            f"🛑 **Ejecución detenida y cola vaciada.**\n"
            f"• Proceso activo cancelado: `{'Sí' if killed else 'No'}`\n"
            f"• Mensajes en cola descartados: `{cleared}`",
        )
        return True

    if data.startswith("q_drop:"):
        job_id = data[7:]
        removed = await execution_manager.drop_queued_job(job_id)
        if removed:
            preview = removed.prompt[:80] + ("..." if len(removed.prompt) > 80 else "")
            await safe_edit(query.message, f"🗑️ **Mensaje retirado de la cola:**\n`{preview}`")
        else:
            await safe_edit(query.message, "ℹ️ Este mensaje ya había iniciado su ejecución o ya no estaba en la cola.")
        return True

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
        return True

    if data == "screen:delete":
        try:
            await query.message.delete()
        except Exception:
            pass
        return True

    if data == "screen:capture":
        ok, result = await capture_wayland_screenshot()
        if not ok:
            await safe_reply(query.message, f"❌ **Error tomando captura:** `{result}`")
            return True
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
        return True

    return False


async def _handle_obsidian_callbacks(query, data: str) -> bool:
    if not data.startswith("ob_"):
        return False

    if data == "ob_home":
        markup, text = build_vault_home_keyboard()
        await safe_edit(query.message, text, reply_markup=markup)
        return True

    if data.startswith("ob_proj:"):
        pidx = int(data.split(":", 1)[1])
        markup, text = build_vault_project_keyboard(pidx)
        await safe_edit(query.message, text, reply_markup=markup)
        return True

    if data.startswith("ob_notes:"):
        _, pidx_s, page_s = data.split(":", 2)
        markup, text = build_notes_list_keyboard(int(pidx_s), page=int(page_s))
        await safe_edit(query.message, text, reply_markup=markup)
        return True

    if data.startswith("ob_nview:"):
        _, pidx_s, nidx_s, sidx_s = data.split(":", 3)
        markup, text, _ = build_note_reader_view(int(pidx_s), int(nidx_s), sec_idx=int(sidx_s))
        await safe_edit(query.message, text, reply_markup=markup)
        return True

    if data.startswith("ob_ndoc:"):
        _, pidx_s, nidx_s = data.split(":", 2)
        projects = list_vault_projects()
        pidx = int(pidx_s)
        if 0 <= pidx < len(projects):
            notes = list_project_notes(str(projects[pidx]["name"]))
            nidx = int(nidx_s)
            if 0 <= nidx < len(notes):
                fn, full = notes[nidx]
                with open(full, "rb") as f:
                    await query.message.reply_document(document=f, filename=fn, caption=f"📒 `{fn}`")
        return True

    if data.startswith("ob_nchk:"):
        _, pidx_s, nidx_s, page_s = data.split(":", 3)
        markup, text = build_note_checklist_view(int(pidx_s), int(nidx_s), page=int(page_s))
        await safe_edit(query.message, text, reply_markup=markup)
        return True

    if data.startswith("ob_ntog:"):
        _, pidx_s, nidx_s, line_s, page_s = data.split(":", 4)
        pidx, nidx, line_i, page_i = int(pidx_s), int(nidx_s), int(line_s), int(page_s)
        projects = list_vault_projects()
        if 0 <= pidx < len(projects):
            notes = list_project_notes(str(projects[pidx]["name"]))
            if 0 <= nidx < len(notes):
                toggle_checklist_line(notes[nidx][1], line_i)
        markup, text = build_note_checklist_view(pidx, nidx, page=page_i)
        await safe_edit(query.message, text, reply_markup=markup)
        return True

    if data.startswith("ob_mods:"):
        pidx = int(data.split(":", 1)[1])
        markup, text = build_prompt_modules_keyboard(pidx)
        await safe_edit(query.message, text, reply_markup=markup)
        return True

    if data.startswith("ob_mlist:"):
        _, pidx_s, midx_s, page_s = data.split(":", 3)
        markup, text = build_module_prompts_keyboard(int(pidx_s), int(midx_s), page=int(page_s))
        await safe_edit(query.message, text, reply_markup=markup)
        return True

    if data.startswith("ob_pview:"):
        _, pidx_s, midx_s, fidx_s, sidx_s = data.split(":", 4)
        markup, text, _ = build_prompt_reader_view(int(pidx_s), int(midx_s), int(fidx_s), sec_idx=int(sidx_s))
        await safe_edit(query.message, text, reply_markup=markup)
        return True

    if data.startswith("ob_pdoc:"):
        _, pidx_s, midx_s, fidx_s = data.split(":", 3)
        pidx, midx, fidx = int(pidx_s), int(midx_s), int(fidx_s)
        projects = list_vault_projects()
        if 0 <= pidx < len(projects):
            proj_name = str(projects[pidx]["name"])
            mods = list_prompt_modules(proj_name)
            if 0 <= midx < len(mods):
                files = list_module_prompts(proj_name, mods[midx][0])
                if 0 <= fidx < len(files):
                    fn, full = files[fidx]
                    with open(full, "rb") as f:
                        await query.message.reply_document(document=f, filename=fn, caption=f"📖 `{fn}`")
        return True

    if data.startswith("ob_pask:"):
        _, pidx_s, midx_s, fidx_s = data.split(":", 3)
        markup, text = build_prompt_launch_confirm(int(pidx_s), int(midx_s), int(fidx_s))
        await safe_edit(query.message, text, reply_markup=markup)
        return True

    if data.startswith("ob_prun:"):
        _, eng, pidx_s, midx_s, fidx_s = data.split(":", 4)
        pidx, midx, fidx = int(pidx_s), int(midx_s), int(fidx_s)
        projects = list_vault_projects()
        if not (0 <= pidx < len(projects)):
            return True
        proj_name = str(projects[pidx]["name"])
        mods = list_prompt_modules(proj_name)
        if not (0 <= midx < len(mods)):
            return True
        mod_name = mods[midx][0]
        files = list_module_prompts(proj_name, mod_name)
        if not (0 <= fidx < len(files)):
            return True

        fn, full_path = files[fidx]
        with open(full_path, "r", encoding="utf-8", errors="replace") as f:
            prompt_text = f.read().strip()

        target_cwd = PROJECT_CODE_MAP.get(proj_name, session_state.current_cwd)
        if os.path.isdir(target_cwd):
            session_state.set_cwd(target_cwd)

        if eng == "cl":
            session_state.set_engine("claude")
            eng_label = "🟠 Claude Code"
        else:
            session_state.set_engine("agy")
            eng_label = "🔵 Antigravity (AGY)"

        chat_guard.touch()
        await safe_edit(
            query.message,
            f"🚀 **Lanzando Prompt `{fn}` en {eng_label}**\n"
            f"📂 **Proyecto (`cwd`):** `{session_state.get_cwd_short_name()}`",
        )
        await execution_manager.submit(prompt_text, query.message)
        return True

    if data.startswith("ob_pdfs:"):
        _, pidx_s, page_s = data.split(":", 2)
        markup, text = build_pdfs_list_keyboard(int(pidx_s), page=int(page_s))
        await safe_edit(query.message, text, reply_markup=markup)
        return True

    if data.startswith("ob_pdf_dl:"):
        _, pidx_s, fidx_s = data.split(":", 2)
        pidx, fidx = int(pidx_s), int(fidx_s)
        projects = list_vault_projects()
        if 0 <= pidx < len(projects):
            pdfs = list_project_pdfs(str(projects[pidx]["name"]))
            if 0 <= fidx < len(pdfs):
                fn, full = pdfs[fidx]
                with open(full, "rb") as f:
                    await query.message.reply_document(document=f, filename=fn, caption=f"📄 `{fn}`")
        return True

    if data.startswith("ob_pdf_gen:"):
        _, pidx_s, fidx_s = data.split(":", 2)
        pidx, fidx = int(pidx_s), int(fidx_s)
        projects = list_vault_projects()
        if 0 <= pidx < len(projects):
            proj_name = str(projects[pidx]["name"])
            pdfs = list_project_pdfs(proj_name)
            if 0 <= fidx < len(pdfs):
                fn, full = pdfs[fidx]
                prompt_text, target_cwd = build_wicode_generator_prompt(proj_name, full)
                session_state.set_cwd(target_cwd)
                chat_guard.touch()
                await safe_edit(
                    query.message,
                    f"⚡ **Ejecutando `wicode-prompt-generator` para `{fn}`**\n"
                    f"📂 **Proyecto:** `{proj_name}`",
                )
                await execution_manager.submit(prompt_text, query.message)
        return True

    if data.startswith("ob_pdf_up:"):
        import shutil
        _, hold_id, pidx_s, action = data.split(":", 3)
        info = pop_pending_pdf(hold_id)
        if not info:
            await safe_edit(query.message, "⌛ Este archivo temporal ya expiró. Reenvía el PDF.")
            return True
        tmp_path = str(info["tmp_path"])
        filename = str(info["filename"])
        if action == "drop":
            if os.path.exists(tmp_path):
                os.remove(tmp_path)
            await safe_edit(query.message, f"🗑️ PDF `{filename}` descartado.")
            return True

        projects = list_vault_projects()
        pidx = int(pidx_s)
        if not (0 <= pidx < len(projects)):
            await safe_edit(query.message, "❌ Proyecto destino no válido.")
            return True
        proj_name = str(projects[pidx]["name"])
        dest_dir = os.path.join(str(projects[pidx]["path"]), "PDF")
        os.makedirs(dest_dir, exist_ok=True)
        dest_path = os.path.join(dest_dir, filename)
        shutil.move(tmp_path, dest_path)

        if action == "save":
            await safe_edit(
                query.message,
                f"✅ **PDF guardado en Obsidian (0 tokens):**\n`{dest_path}`",
            )
            return True

        prompt_text, target_cwd = build_wicode_generator_prompt(proj_name, dest_path)
        session_state.set_cwd(target_cwd)
        chat_guard.touch()
        await safe_edit(
            query.message,
            f"⚡ **PDF guardado en `{proj_name}/PDF/{filename}`.**\n"
            f"🚀 *Iniciando `wicode-prompt-generator` (Fase 1 Nota Markdown + Fase 2 Prompts T01..TXX)...*",
        )
        await execution_manager.submit(prompt_text, query.message)
        return True

    return False


async def handle_callback_query(update: Update, context: ContextTypes.DEFAULT_TYPE):
    query = update.callback_query
    if not query or not is_authorized(update):
        return

    await query.answer()
    data = query.data or ""
    if not data or data == "res_noop":
        return

    for handler in (
        _handle_obsidian_callbacks,
        _handle_claude_callbacks,
        _handle_chat_guard_callbacks,
        _handle_project_callbacks,
        _handle_agy_chats_callbacks,
        _handle_model_and_system_callbacks,
        _handle_queue_and_screen_callbacks,
    ):
        if await handler(query, data):
            return
