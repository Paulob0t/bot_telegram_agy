import os
import sys
import shutil
import logging

IS_WINDOWS = sys.platform == "win32"
PROJECT_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
ENV_FILE_NAME = os.environ.get("BOT_ENV_FILE", ".env")
ENV_CANDIDATES = [
    os.path.join(PROJECT_ROOT, ENV_FILE_NAME),
    os.path.join(PROJECT_ROOT, ".env"),
    os.path.expanduser("~/.config/telegram-bot/.env"),
]


def load_dotenv_files():
    """Carga variables de entorno desde el archivo .env sin dependencias externas."""
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
                logging.warning("No se pudo leer %s: %s", env_path, exc)


load_dotenv_files()

TOKEN = os.environ.get("TELEGRAM_BOT_TOKEN", "").strip()
AUTHORIZED_USER_ID = int(os.environ.get("TELEGRAM_AUTHORIZED_USER_ID", "0") or "0")

if not TOKEN or not AUTHORIZED_USER_ID:
    raise RuntimeError(
        "Faltan credenciales TELEGRAM_BOT_TOKEN o TELEGRAM_AUTHORIZED_USER_ID. "
        "Configúralas en el archivo .env."
    )

HOME_DIR = os.environ.get("BOT_HOME_DIR", os.path.expanduser("~"))
_default_projects = r"C:\Proyectos" if IS_WINDOWS else "/run/media/paulobot/Datos/Projects"
_default_obsidian = os.path.join(HOME_DIR, "Documents", "wicode") if IS_WINDOWS else "/run/media/paulobot/Datos/Documents/wicode"
PROJECTS_BASE_DIR = os.environ.get("BOT_PROJECTS_DIR", _default_projects)
OBSIDIAN_VAULT_DIR = os.environ.get("BOT_OBSIDIAN_DIR", _default_obsidian)

LONG_MESSAGE_THRESHOLD = int(os.environ.get("BOT_LONG_MESSAGE_THRESHOLD", "400"))
LONG_MESSAGE_WAIT_SECONDS = float(os.environ.get("BOT_LONG_MESSAGE_WAIT_SECONDS", "4"))
CHAT_IDLE_MINUTES = float(os.environ.get("BOT_CHAT_IDLE_MINUTES", "30"))
BOT_MAX_RAM_MB = int(os.environ.get("BOT_MAX_RAM_MB", "120"))

BASE_CLI_DIR = os.path.expanduser("~/.gemini/antigravity-cli")
DB_PATH = os.path.join(BASE_CLI_DIR, "conversation_summaries.db")
USAGE_CACHE_PATH = os.path.join(BASE_CLI_DIR, "last_token_usage.json")
STATE_CACHE_PATH = os.path.join(BASE_CLI_DIR, "telegram_bot_state.json")


def _resolve_binary(env_var: str, name: str, win_fallbacks: list, unix_fallbacks: list) -> str:
    if os.environ.get(env_var):
        return os.environ[env_var]
    found = shutil.which(name)
    if found:
        return found
    for candidate in (win_fallbacks if IS_WINDOWS else unix_fallbacks):
        expanded = os.path.expanduser(candidate)
        if os.path.isfile(expanded):
            return expanded
    return name


AGY_BIN = _resolve_binary(
    "AGY_BIN",
    "agy.exe" if IS_WINDOWS else "agy",
    [r"~\AppData\Local\agy\bin\agy.exe", r"~\.local\bin\agy.exe"],
    ["~/.local/bin/agy", "~/.gemini/antigravity-cli/bin/agy"],
)
CLAUDE_BIN = _resolve_binary(
    "CLAUDE_BIN",
    "claude.exe" if IS_WINDOWS else "claude",
    [r"~\AppData\Roaming\npm\claude.cmd", r"~\.local\bin\claude.exe"],
    ["~/.local/bin/claude"],
)
CLAUDE_PROJECTS_DIR = os.path.expanduser("~/.claude/projects")

logging.basicConfig(
    format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
    level=logging.INFO,
)
logger = logging.getLogger("telegram_paulobot")
logging.getLogger("httpx").setLevel(logging.WARNING)


def apply_process_memory_guard(max_mb: int = BOT_MAX_RAM_MB) -> None:
    """Ajusta el recolector de basura de Python y limita el working set del proceso en Windows."""
    import gc
    gc.set_threshold(400, 5, 5)
    if IS_WINDOWS and max_mb > 0:
        try:
            import ctypes
            kernel32 = ctypes.windll.kernel32
            handle = kernel32.GetCurrentProcess()
            min_ws = 8 * 1024 * 1024
            max_ws = max_mb * 1024 * 1024
            kernel32.SetProcessWorkingSetSize(handle, ctypes.c_size_t(min_ws), ctypes.c_size_t(max_ws))
        except Exception:
            pass


def trim_process_memory() -> None:
    """Libera memoria no referenciada tras finalizar una ejecución pesada."""
    import gc
    gc.collect()
    if IS_WINDOWS:
        try:
            import ctypes
            ctypes.windll.psapi.EmptyWorkingSet(ctypes.windll.kernel32.GetCurrentProcess())
        except Exception:
            pass
