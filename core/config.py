import os
import shutil
import logging

PROJECT_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
ENV_CANDIDATES = [
    os.path.join(PROJECT_ROOT, ".env"),
    os.path.expanduser("~/.config/telegram-bot/.env"),
]


def load_dotenv_files():
    """Carga variables de entorno desde el archivo .env (permisos 600) sin dependencias externas."""
    for env_path in ENV_CANDIDATES:
        if os.path.isfile(env_path):
            try:
                with open(env_path, "r", encoding="utf-8") as f:
                    for raw_line in f:
                        line = raw_line.strip()
                        if not line or line.startswith("#") or "=" not in line:
                            continue
                        key, val = line.split("=", 1)
                        key = key.strip()
                        val = val.strip().strip('"').strip("'")
                        if key and key not in os.environ:
                            os.environ[key] = val
            except Exception as exc:
                # logger aún no existe aquí; usar logging raíz
                logging.warning("No se pudo leer %s: %s", env_path, exc)


load_dotenv_files()

TOKEN = os.environ.get("TELEGRAM_BOT_TOKEN", "").strip()
AUTHORIZED_USER_ID = int(os.environ.get("TELEGRAM_AUTHORIZED_USER_ID", "0") or "0")

if not TOKEN or not AUTHORIZED_USER_ID:
    raise RuntimeError(
        "Faltan credenciales TELEGRAM_BOT_TOKEN o TELEGRAM_AUTHORIZED_USER_ID. "
        "Configúralas en el archivo .env (chmod 600)."
    )

HOME_DIR = os.environ.get("BOT_HOME_DIR", os.path.expanduser("~"))
PROJECTS_BASE_DIR = os.environ.get("BOT_PROJECTS_DIR", "/run/media/paulobot/Datos/Projects")

# Mensajes largos: Telegram parte textos > 4096 chars en varios mensajes.
# Si un mensaje supera LONG_MESSAGE_THRESHOLD, se espera LONG_MESSAGE_WAIT_SECONDS
# por fragmentos adicionales y se unen en un solo prompt.
LONG_MESSAGE_THRESHOLD = int(os.environ.get("BOT_LONG_MESSAGE_THRESHOLD", "400"))
LONG_MESSAGE_WAIT_SECONDS = float(os.environ.get("BOT_LONG_MESSAGE_WAIT_SECONDS", "4"))

# Minutos de inactividad tras los cuales el bot pregunta en qué chat enviar
# el siguiente mensaje (0 = desactivado).
CHAT_IDLE_MINUTES = float(os.environ.get("BOT_CHAT_IDLE_MINUTES", "30"))

BASE_CLI_DIR = os.path.expanduser("~/.gemini/antigravity-cli")
DB_PATH = os.path.join(BASE_CLI_DIR, "conversation_summaries.db")
USAGE_CACHE_PATH = os.path.join(BASE_CLI_DIR, "last_token_usage.json")
STATE_CACHE_PATH = os.path.join(BASE_CLI_DIR, "telegram_bot_state.json")

AGY_BIN = os.environ.get("AGY_BIN") or shutil.which("agy") or os.path.expanduser("~/.local/bin/agy")

logging.basicConfig(
    format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
    level=logging.INFO,
)
logger = logging.getLogger("telegram_paulobot")
# Evitar que httpx imprima la URL completa con el token del bot en cada getUpdates
logging.getLogger("httpx").setLevel(logging.WARNING)
