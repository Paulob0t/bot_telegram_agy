import sys
from telegram import Update, BotCommand
from telegram.ext import (
    Application,
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


async def post_init(application: Application) -> None:
    """Sincroniza el menú de comandos de Telegram al iniciar el bot."""
    commands = [
        BotCommand("start", "Panel principal y ayuda"),
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
        BotCommand("screen", "Captura de pantalla de la PC"),
        BotCommand("delete", "Eliminar conversaciones guardadas"),
        BotCommand("off", "Apagar servicio del bot"),
    ]
    try:
        await application.bot.set_my_commands(commands)
        logger.info("Menú de comandos de Telegram actualizado (%d comandos).", len(commands))
    except Exception as e:
        logger.warning("No se pudo actualizar el menú de comandos: %s", e)


def main():
    print(f"Iniciando Bot de Telegram Modular AGY (Autorizado: {AUTHORIZED_USER_ID})...")
    application = (
        ApplicationBuilder()
        .token(TOKEN)
        .concurrent_updates(True)
        .post_init(post_init)
        .build()
    )

    application.add_error_handler(global_error_handler)

    # Guardia global de autorización (grupo -1 corre antes que todos los handlers)
    application.add_handler(TypeHandler(Update, auth_guard), group=-1)

    # Comandos de Proyecto y Captura (/screen)
    application.add_handler(
        CommandHandler(["project", "cd", "workspace", "dir", "repo", "proyectos"], cmd_project)
    )
    application.add_handler(
        CommandHandler(["screen", "ss", "screenshot", "captura", "pantalla"], cmd_screen)
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

    # Mensajes de texto normales y fotos
    application.add_handler(MessageHandler((filters.TEXT | filters.PHOTO) & ~filters.COMMAND, handle_message))

    print("Bot listo y a la escucha (Modular + Proyectos + Cola Concurrente).")
    application.run_polling()


if __name__ == "__main__":
    main()
