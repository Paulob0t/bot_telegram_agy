import os
import time
import asyncio
from telegram import Update, InlineKeyboardButton, InlineKeyboardMarkup
from telegram.ext import ContextTypes
from core.config import HOME_DIR, PROJECTS_BASE_DIR
from core.state import session_state
from core.utils import is_authorized, safe_edit, safe_reply, format_uptime, get_git_branch
from core.tunnels import tunnel_manager
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
    sync_models_from_cli,
)
from core.dev_services import dev_manager, build_dev_dashboard
from core.git_tools import build_git_dashboard, run_git_cmd
from core.desktop_tools import (
    capture_wayland_screenshot,
    build_screenshot_keyboard,
    build_dnd_dashboard,
)
from core.executor import execution_manager


async def shutdown_bot_service():
    await asyncio.sleep(1.0)
    os.system("systemctl --user stop telegram-bot.service")


async def handle_callback_query(update: Update, context: ContextTypes.DEFAULT_TYPE):
    query = update.callback_query
    if not query or not is_authorized(update):
        return

    await query.answer()
    data = query.data

    if data == "res_noop":
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
    # ☁️ CALLBACKS DE TÚNELES CLOUDFLARE
    # =========================================================
    if data.startswith("tun_stop:"):
        port = int(data[9:])
        await tunnel_manager.stop_tunnel(port)
        await safe_edit(query.message, f"🛑 **Túnel Cloudflare en puerto `{port}` cerrado con éxito.**")
        return

    if data == "tun_stop_all":
        count = await tunnel_manager.stop_all_tunnels()
        await safe_edit(query.message, f"🛑 **Se cerraron todos los túneles de Cloudflare ({count} en total).**")
        return

    if data == "tun_list":
        tunnels = tunnel_manager.list_tunnels()
        if not tunnels:
            await safe_edit(query.message, "ℹ️ No hay túneles de Cloudflare activos actualmente.")
            return
        keyboard = []
        text_lines = ["☁️ **Túneles Cloudflare Activos (trycloudflare.com):**\n"]
        for t in tunnels:
            p = t["port"]
            u = t["url"]
            up = format_uptime(t["uptime_seconds"])
            text_lines.append(f"• 🔌 **Puerto `{p}`** (Activo: `{up}`)\n  🔗 `{u}`\n")
            keyboard.append([
                InlineKeyboardButton(f"🌐 Abrir :{p}", url=u),
                InlineKeyboardButton(f"🛑 Cerrar :{p}", callback_data=f"tun_stop:{p}"),
            ])
        keyboard.append([InlineKeyboardButton("🚫 Cerrar Todos", callback_data="tun_stop_all")])
        await safe_edit(query.message, "\n".join(text_lines), reply_markup=InlineKeyboardMarkup(keyboard))
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
            await safe_edit(query.message, "✅ **Configurado y guardado:** Ahora enviarás mensajes a la **última conversación activa**.")
        elif target == "NEW":
            session_state.set_conversation("NEW", "➕ Nuevo chat independiente")
            await safe_edit(query.message, "✅ **Configurado y guardado:** El siguiente mensaje creará un **nuevo chat limpio**.")
        else:
            title = get_conversation_title(target)
            session_state.set_conversation(target, f"💬 {title}")
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
            f"• 🪟 **Ventana de contexto:** `{ctx_win:,}` tokens",
        )
        return

    if data.startswith("eff:"):
        target = data[4:]
        session_state.set_effort(target)
        await safe_edit(query.message, f"✅ **Nivel de razonamiento guardado:** `{target.upper()}`")
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
    # 🛠️ CALLBACKS DEL GESTOR DE SERVICIOS DE DESARROLLO (/dev)
    # =========================================================
    if data == "dev:refresh":
        markup, text = build_dev_dashboard()
        await safe_edit(query.message, text, reply_markup=markup)
        return

    if data == "dev:start_all":
        await safe_edit(
            query.message,
            "🚀 **Iniciando WiCode Fullstack...**\n"
            "⏳ *Levantando Cloud SQL Proxy (:5433), Backend (:3000) y Frontend (:5173)...*",
        )
        try:
            await dev_manager.start_wicode_fullstack()
            markup, text = build_dev_dashboard()
            await safe_edit(
                query.message,
                "✅ **Secuencia de inicio Fullstack completada.**\n\n" + text,
                reply_markup=markup,
            )
        except Exception as e:
            markup, text = build_dev_dashboard()
            await safe_edit(
                query.message,
                f"❌ **Error iniciando servicios:** `{e}`\n\n" + text,
                reply_markup=markup,
            )
        return

    if data == "dev:stop_all":
        await safe_edit(
            query.message,
            "🛑 **Deteniendo WiCode Fullstack...**\n"
            "⏳ *Liberando puertos 5173, 3000 y 5433...*",
        )
        await dev_manager.stop_wicode_fullstack()
        markup, text = build_dev_dashboard()
        await safe_edit(
            query.message,
            "🛑 **Todos los servicios de WiCode fueron detenidos.**\n\n" + text,
            reply_markup=markup,
        )
        return

    if data.startswith("dev:toggle:"):
        svc_key = data[11:]
        running, _ = dev_manager.is_service_running(svc_key)
        svc_obj = dev_manager.services.get(svc_key)
        svc_label = svc_obj.name if svc_obj else svc_key

        if running:
            await safe_edit(query.message, f"🛑 *Deteniendo {svc_label}...*")
            await dev_manager.stop_service(svc_key)
            banner = f"🛑 **{svc_label} detenido.**\n\n"
        else:
            await safe_edit(query.message, f"▶️ *Iniciando {svc_label}...*")
            await dev_manager.start_service(svc_key, wait_port_seconds=4.0)
            banner = f"▶️ **{svc_label} iniciado.**\n\n"

        markup, text = build_dev_dashboard()
        await safe_edit(query.message, banner + text, reply_markup=markup)
        return

    if data == "dev:tunnels_both":
        await safe_edit(
            query.message,
            "☁️ **Creando túneles Cloudflare para Frontend (`:5173`) y Backend (`:3000`)...**\n"
            "⏳ *Obtieniendo URLs públicas HTTPS...*",
        )
        urls_msg = []
        for port, label in ((5173, "🎨 Frontend"), (3000, "⚙️ Backend")):
            try:
                url = await tunnel_manager.start_tunnel(port, timeout=18.0)
                urls_msg.append(f"• {label} (`:{port}`): `{url}`")
            except Exception as e:
                urls_msg.append(f"• {label} (`:{port}`): ❌ `{e}`")

        markup, text = build_dev_dashboard()
        summary_header = "🎉 **Túneles WiCode Configurados:**\n" + "\n".join(urls_msg) + "\n\n"
        await safe_edit(query.message, summary_header + text, reply_markup=markup)
        return

    if data.startswith("dev:logs:"):
        svc_key = data[9:]
        log_report = dev_manager.get_service_logs(svc_key, max_lines=25)
        back_kb = InlineKeyboardMarkup([
            [
                InlineKeyboardButton("🔄 Refrescar Log", callback_data=f"dev:logs:{svc_key}"),
                InlineKeyboardButton("🔙 Volver a /dev", callback_data="dev:refresh"),
            ]
        ])
        await safe_edit(query.message, log_report, reply_markup=back_kb)
        return

    if data == "dev:errors":
        err_report = dev_manager.get_recent_errors_report()
        back_kb = InlineKeyboardMarkup([
            [
                InlineKeyboardButton("🔄 Re-escanear Errores", callback_data="dev:errors"),
                InlineKeyboardButton("🔙 Volver a /dev", callback_data="dev:refresh"),
            ]
        ])
        await safe_edit(query.message, err_report, reply_markup=back_kb)
        return

    # =========================================================
    # 🐙 CALLBACKS DE COMANDOS RÁPIDOS DE GIT (/git)
    # =========================================================
    if data == "git:status":
        _, out = await run_git_cmd(["status", "-sb"])
        markup, text = await build_git_dashboard(
            custom_title="📊 Estado del Repositorio (`git status -sb`)",
            cmd_output=out or "Árbol de trabajo limpio.",
        )
        await safe_edit(query.message, text, reply_markup=markup)
        return

    if data == "git:diff":
        _, out = await run_git_cmd(["diff", "--stat"])
        if not out:
            _, out = await run_git_cmd(["diff", "--cached", "--stat"])
            title = "📈 Cambios en Stage (`git diff --cached --stat`)"
        else:
            title = "📈 Resumen de Cambios (`git diff --stat`)"
        markup, text = await build_git_dashboard(
            custom_title=title,
            cmd_output=out or "No hay diferencias pendientes en archivos rastreados.",
        )
        await safe_edit(query.message, text, reply_markup=markup)
        return

    if data == "git:log":
        _, out = await run_git_cmd(["log", "-n", "5", "--oneline", "--decorate", "--graph"])
        markup, text = await build_git_dashboard(
            custom_title="📜 Últimos 5 Commits (`git log -n 5`)",
            cmd_output=out or "Sin historial de commits.",
        )
        await safe_edit(query.message, text, reply_markup=markup)
        return

    if data == "git:branches":
        _, out = await run_git_cmd(["branch", "-vv"])
        markup, text = await build_git_dashboard(
            custom_title="🌿 Ramas Locales (`git branch -vv`)",
            cmd_output=out or "No se encontraron ramas.",
        )
        await safe_edit(query.message, text, reply_markup=markup)
        return

    if data == "git:pull":
        await safe_edit(query.message, "⬇️ *Ejecutando `git pull` en el repositorio activo...*")
        code, out = await run_git_cmd(["pull"], timeout=25.0)
        icon = "✅" if code == 0 else "⚠️"
        markup, text = await build_git_dashboard(
            custom_title=f"{icon} Resultado de `git pull`",
            cmd_output=out or "Completado.",
        )
        await safe_edit(query.message, text, reply_markup=markup)
        return

    if data == "git:push_ask":
        cwd = session_state.current_cwd
        short_name = session_state.get_cwd_short_name(cwd)
        branch = get_git_branch(cwd) or "HEAD"
        _, unpushed = await run_git_cmd(["log", "@{u}..", "--oneline"])
        preview = unpushed if unpushed else "(Se enviará la rama actual al remoto origin)"

        confirm_kb = InlineKeyboardMarkup([
            [
                InlineKeyboardButton("✅ Sí, hacer Git Push", callback_data="git:push_confirm"),
                InlineKeyboardButton("❌ Cancelar", callback_data="git:status"),
            ]
        ])
        await safe_edit(
            query.message,
            f"⚠️ **Confirmar `git push`**\n\n"
            f"• 📂 **Proyecto:** `{short_name}`\n"
            f"• 🌿 **Rama:** `{branch}`\n\n"
            f"**Commits por subir:**\n```\n{preview[:1500]}\n```\n"
            f"¿Deseas subir estos cambios al repositorio remoto?",
            reply_markup=confirm_kb,
        )
        return

    if data == "git:push_confirm":
        await safe_edit(query.message, "⬆️ *Ejecutando `git push`...*")
        branch = get_git_branch(session_state.current_cwd)
        code, out = await run_git_cmd(["push"], timeout=30.0)
        if code != 0 and branch and "has no upstream branch" in out:
            code, out = await run_git_cmd(["push", "-u", "origin", branch], timeout=30.0)
        icon = "✅" if code == 0 else "❌"
        markup, text = await build_git_dashboard(
            custom_title=f"{icon} Resultado de `git push`",
            cmd_output=out or "Push completado con éxito.",
        )
        await safe_edit(query.message, text, reply_markup=markup)
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

    # =========================================================
    # 🔔 CALLBACKS DE MODO NO MOLESTAR (/dnd)
    # =========================================================
    if data == "dnd:refresh":
        markup, text = build_dnd_dashboard()
        await safe_edit(query.message, text, reply_markup=markup)
        return

    if data.startswith("dnd:set:"):
        mode = data[8:]
        session_state.set_dnd_mode(mode)
        markup, text = build_dnd_dashboard()
        await safe_edit(query.message, text, reply_markup=markup)
        return
