import sys
from telegram import Update
from telegram.ext import (
    ApplicationBuilder,
    ApplicationHandlerStop,
    ContextTypes,
    CommandHandler,
    MessageHandler,
    CallbackQueryHandler,
    TypeHandler,
    filters,
)
from core.config import TOKEN, AUTHORIZED_USER_ID, logger
from core.handlers.commands import (
    cmd_start,
    cmd_project,
    cmd_screen,
    cmd_dnd,
    cmd_usage,
    cmd_resume,
    cmd_delete,
    cmd_model,
    cmd_effort,
    cmd_continue,
    cmd_new,
    cmd_current,
    cmd_status,
    cmd_queue,
    cmd_cancel,
    cmd_off,
    handle_message,
)
from core.handlers.callbacks import handle_callback_query

# Forzar line buffering en stdout para journalctl / systemd logs
sys.stdout.reconfigure(line_buffering=True)


async def global_error_handler(update: object, context: ContextTypes.DEFAULT_TYPE) -> None:
    logger.error("Excepción en bot de Telegram:", exc_info=context.error)


async def auth_guard(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    """Corta cualquier update de usuarios no autorizados antes de llegar a los handlers."""
    user = update.effective_user
    if user is None or user.id != AUTHORIZED_USER_ID:
        if user is not None:
            logger.warning("Update bloqueado de usuario no autorizado: %s", user.id)
        raise ApplicationHandlerStop


def main():
    print(f"Iniciando Bot de Telegram Modular AGY (Autorizado: {AUTHORIZED_USER_ID})...")
    application = ApplicationBuilder().token(TOKEN).concurrent_updates(True).build()

    application.add_error_handler(global_error_handler)

    # Guardia global de autorización (grupo -1 corre antes que todos los handlers)
    application.add_handler(TypeHandler(Update, auth_guard), group=-1)

    # Comandos de Proyecto, Captura (/screen) y DND (/dnd)
    application.add_handler(
        CommandHandler(["project", "cd", "workspace", "dir", "repo", "proyectos"], cmd_project)
    )
    application.add_handler(
        CommandHandler(["screen", "ss", "screenshot", "captura", "pantalla"], cmd_screen)
    )
    application.add_handler(
        CommandHandler(["dnd", "notificaciones", "mute", "nomolestar"], cmd_dnd)
    )

    # Comandos Generales de AGY
    application.add_handler(CommandHandler(["start", "help", "ayuda"], cmd_start))
    application.add_handler(CommandHandler(["usage", "tokens", "consumo"], cmd_usage))
    application.add_handler(CommandHandler(["resume", "chats", "historial"], cmd_resume))
    application.add_handler(CommandHandler(["delete", "borrar", "eliminar"], cmd_delete))
    application.add_handler(CommandHandler(["model", "modelo"], cmd_model))
    application.add_handler(CommandHandler(["effort", "esfuerzo", "speed"], cmd_effort))
    application.add_handler(CommandHandler(["continue", "continuar"], cmd_continue))
    application.add_handler(CommandHandler(["new", "nuevo"], cmd_new))
    application.add_handler(CommandHandler(["current", "actual"], cmd_current))
    application.add_handler(CommandHandler(["status", "estado"], cmd_status))
    application.add_handler(CommandHandler(["queue", "cola"], cmd_queue))
    application.add_handler(CommandHandler(["cancel", "stop", "cancelar"], cmd_cancel))
    application.add_handler(CommandHandler(["off", "power", "apagar", "stopbot", "shutdown"], cmd_off))

    # Callbacks interactivos
    application.add_handler(CallbackQueryHandler(handle_callback_query))

    # Mensajes de texto normales
    application.add_handler(MessageHandler(filters.TEXT & ~filters.COMMAND, handle_message))

    print("Bot listo y a la escucha (Modular + Proyectos + Cola Concurrente).")
    application.run_polling()


if __name__ == "__main__":
    main()
