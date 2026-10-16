import traceback
from telegram import Update, InlineKeyboardButton, InlineKeyboardMarkup
from telegram.ext import ContextTypes
from core.config import logger
from core.utils import is_authorized, safe_reply, safe_edit, format_uptime
from core.tunnels import tunnel_manager


async def cmd_tunnel(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """Comando /tunnel <puerto> o /share <puerto> para exponer puertos locales vía Cloudflare."""
    if not is_authorized(update):
        return

    args = context.args
    if not args or len(args) == 0:
        await safe_reply(
            update.message,
            "💡 **Uso del comando Túnel (Cloudflare):**\n\n"
            "`/tunnel <puerto>` o `/share <puerto>`\n\n"
            "**Ejemplos:**\n"
            "• `/tunnel 5173` *(Vite / React)*\n"
            "• `/tunnel 3000` *(Next.js / Node)*\n"
            "• `/tunnel 8080` *(Backend / APIs)*\n"
            "• `/tunnel 8000` *(FastAPI / Django / Rails)*",
        )
        return

    port_str = args[0].strip()
    if not port_str.isdigit():
        await safe_reply(update.message, f"❌ El puerto debe ser numérico. Recibido: `{port_str}`")
        return

    port = int(port_str)
    if not (1 <= port <= 65535):
        await safe_reply(update.message, f"❌ El puerto `{port}` está fuera de rango (1 - 65535).")
        return

    status_msg = await safe_reply(
        update.message,
        f"☁️ **Creando túnel seguro de Cloudflare para el puerto `{port}`...**\n⏳ *Iniciando cloudflared...*",
    )

    try:
        url = await tunnel_manager.start_tunnel(port, timeout=20.0)

        keyboard = [
            [InlineKeyboardButton("🌐 Abrir en Navegador / Celular", url=url)],
            [
                InlineKeyboardButton(f"🛑 Detener Túnel ({port})", callback_data=f"tun_stop:{port}"),
                InlineKeyboardButton("📋 Ver Todos", callback_data="tun_list"),
            ],
        ]
        reply_markup = InlineKeyboardMarkup(keyboard)

        msg = (
            "🎉 **¡Túnel Cloudflare Activo!** ☁️🚀\n\n"
            f"• 🔌 **Puerto Local:** `http://localhost:{port}`\n"
            f"• 🔗 **URL Pública HTTPS:**\n`{url}`\n\n"
            "📱 *Puedes abrir el enlace directamente en tu celular o compartirlo con quien quieras.*"
        )
        if status_msg:
            await safe_edit(status_msg, msg, reply_markup=reply_markup)
        else:
            await safe_reply(update.message, msg, reply_markup=reply_markup)

    except TimeoutError as te:
        err = f"⚠️ {te}\n\n*Asegúrate de que el servidor web esté corriendo en el puerto {port} y que tu conexión a internet esté activa.*"
        if status_msg:
            await safe_edit(status_msg, err)
        else:
            await safe_reply(update.message, err)
    except Exception as e:
        logger.error(f"Error abriendo túnel Cloudflare en {port}: {traceback.format_exc()}")
        err = f"❌ **Error iniciando túnel Cloudflare:**\n`{e}`"
        if status_msg:
            await safe_edit(status_msg, err)
        else:
            await safe_reply(update.message, err)


async def cmd_tunnels_status(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """Comando /tunnels o /tunnels_status para listar túneles activos."""
    if not is_authorized(update):
        return

    tunnels = tunnel_manager.list_tunnels()
    if not tunnels:
        await safe_reply(
            update.message,
            "ℹ️ **No hay túneles de Cloudflare activos en este momento.**\n\n"
            "Usa `/tunnel <puerto>` (ej. `/tunnel 5173`) para crear uno nuevo.",
        )
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

    keyboard.append([
        InlineKeyboardButton("🚫 Detener Todos los Túneles", callback_data="tun_stop_all")
    ])

    reply_markup = InlineKeyboardMarkup(keyboard)
    await safe_reply(update.message, "\n".join(text_lines), reply_markup=reply_markup)


async def cmd_stop_tunnel(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """Comando /stop_tunnel <puerto> para detener un túnel específico."""
    if not is_authorized(update):
        return

    args = context.args
    if not args or len(args) == 0:
        tunnels = tunnel_manager.list_tunnels()
        if not tunnels:
            await safe_reply(update.message, "ℹ️ No hay ningún túnel de Cloudflare activo para detener.")
            return

        keyboard = [
            [InlineKeyboardButton(f"🛑 Cerrar túnel :{t['port']}", callback_data=f"tun_stop:{t['port']}")]
            for t in tunnels
        ]
        keyboard.append([InlineKeyboardButton("🚫 Cerrar Todos", callback_data="tun_stop_all")])
        await safe_reply(
            update.message,
            "👇 **Selecciona el túnel que deseas cerrar:**",
            reply_markup=InlineKeyboardMarkup(keyboard),
        )
        return

    port_str = args[0].strip()
    if not port_str.isdigit():
        await safe_reply(update.message, "❌ El puerto debe ser numérico. Ejemplo: `/stop_tunnel 5173`")
        return

    port = int(port_str)
    success = await tunnel_manager.stop_tunnel(port)
    if success:
        await safe_reply(update.message, f"✅ **Túnel Cloudflare en el puerto `{port}` cerrado y liberado.**")
    else:
        await safe_reply(update.message, f"ℹ️ No se encontró ningún túnel activo en el puerto `{port}`.")


async def cmd_stop_all_tunnels(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """Comando /stop_all para detener todos los túneles."""
    if not is_authorized(update):
        return

    count = await tunnel_manager.stop_all_tunnels()
    if count > 0:
        await safe_reply(
            update.message,
            f"🛑 **Se cerraron {count} túnel(es) de Cloudflare y se liberaron todos los subprocesos.**",
        )
    else:
        await safe_reply(update.message, "ℹ️ No había túneles activos para cerrar.")
