import os
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
            except Exception:
                pass


load_dotenv_files()

TOKEN = os.environ.get("TELEGRAM_BOT_TOKEN", "").strip()
AUTHORIZED_USER_ID = int(os.environ.get("TELEGRAM_AUTHORIZED_USER_ID", "0") or "0")

if not TOKEN or not AUTHORIZED_USER_ID:
    raise RuntimeError(
        "Faltan credenciales TELEGRAM_BOT_TOKEN o TELEGRAM_AUTHORIZED_USER_ID. "
        "Configúralas en el archivo .env (chmod 600)."
    )

HOME_DIR = "/home/paulobot"
PROJECTS_BASE_DIR = "/run/media/paulobot/Datos/Projects"

BASE_CLI_DIR = os.path.expanduser("~/.gemini/antigravity-cli")
DB_PATH = os.path.join(BASE_CLI_DIR, "conversation_summaries.db")
USAGE_CACHE_PATH = os.path.join(BASE_CLI_DIR, "last_token_usage.json")
STATE_CACHE_PATH = os.path.join(BASE_CLI_DIR, "telegram_bot_state.json")

AGY_BIN = "/home/paulobot/.local/bin/agy"

logging.basicConfig(
    format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
    level=logging.INFO,
)
logger = logging.getLogger("telegram_paulobot")
# Evitar que httpx imprima la URL completa con el token del bot en cada getUpdates
logging.getLogger("httpx").setLevel(logging.WARNING)
