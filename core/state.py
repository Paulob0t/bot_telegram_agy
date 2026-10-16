import os
import json
import time
import sqlite3
import tempfile
from typing import Optional
from core.config import HOME_DIR, DB_PATH, USAGE_CACHE_PATH, STATE_CACHE_PATH, logger

VALID_EFFORTS = ("low", "medium", "high", "xhigh", "max")
VALID_DND_MODES = ("off", "auto", "on")


class BotSessionState:
    """
    Estado centralizado de la sesión de AGY con persistencia atómica en disco
    (~/.gemini/antigravity-cli/telegram_bot_state.json).
    """

    def __init__(self):
        self.current_conversation_id: Optional[str] = None  # None = último (-c), 'NEW' = nuevo, o UUID
        self.current_conversation_title: str = "🔄 Continuar último chat activo (-c)"
        self.current_model: Optional[str] = None  # None = por defecto o ID específico
        self.current_effort: str = "low"  # low, medium, high, xhigh, max
        self.current_cwd: str = HOME_DIR  # Directorio de trabajo activo para AGY
        self.dnd_mode: str = "off"  # off = enviar siempre, auto = solo con hyprlock/bloqueado, on = silenciar
        self.updated_at: Optional[float] = None
        self.last_usage_data: dict = {}

        self._load_state()
        self._load_usage()

    def _verify_conversation_exists(self, conv_id: str) -> Optional[str]:
        """Verifica en SQLite si la conversación existe y retorna su título actualizado."""
        if not os.path.exists(DB_PATH) or not conv_id:
            return None
        try:
            conn = sqlite3.connect(DB_PATH)
            cur = conn.cursor()
            cur.execute(
                "SELECT title FROM conversation_summaries WHERE conversation_id = ?",
                (conv_id,),
            )
            row = cur.fetchone()
            conn.close()
            if row and row[0]:
                return row[0]
        except Exception:
            pass
        return None

    def _load_state(self):
        if not os.path.exists(STATE_CACHE_PATH):
            self.save_state()
            return

        try:
            with open(STATE_CACHE_PATH, "r", encoding="utf-8") as f:
                data = json.load(f)

            # 1. Restaurar esfuerzo validado
            effort = str(data.get("current_effort", "low")).lower()
            self.current_effort = effort if effort in VALID_EFFORTS else "low"

            # 2. Restaurar modelo
            model = data.get("current_model", None)
            self.current_model = str(model).strip() if model else None

            # 3. Restaurar directorio de trabajo (cwd)
            saved_cwd = data.get("current_cwd", HOME_DIR)
            if saved_cwd and os.path.isdir(saved_cwd):
                self.current_cwd = os.path.abspath(saved_cwd)
            else:
                self.current_cwd = HOME_DIR

            # 4. Restaurar conversación activa y sincronizar título con SQLite
            saved_conv_id = data.get("current_conversation_id", None)
            if saved_conv_id in (None, "LATEST"):
                self.current_conversation_id = None
                self.current_conversation_title = "🔄 Continuar último chat activo (-c)"
            elif saved_conv_id == "NEW":
                self.current_conversation_id = "NEW"
                self.current_conversation_title = "➕ Nuevo chat independiente"
            else:
                db_title = self._verify_conversation_exists(saved_conv_id)
                if db_title:
                    self.current_conversation_id = saved_conv_id
                    self.current_conversation_title = f"💬 {db_title}"
                else:
                    # Si el chat ya fue borrado externamente, regresar a último activo
                    self.current_conversation_id = None
                    self.current_conversation_title = "🔄 Continuar último chat activo (-c)"

            # 5. Restaurar modo No Molestar (DND)
            saved_dnd = str(data.get("dnd_mode", "off")).lower()
            self.dnd_mode = saved_dnd if saved_dnd in VALID_DND_MODES else "off"

            self.updated_at = data.get("updated_at", time.time())
            logger.info(
                f"Estado restaurado desde {STATE_CACHE_PATH}: "
                f"cwd='{self.current_cwd}', model='{self.current_model or 'DEFAULT'}', "
                f"effort='{self.current_effort}', dnd='{self.dnd_mode}'"
            )
        except Exception as e:
            logger.warning(f"No se pudo cargar estado previo del bot ({STATE_CACHE_PATH}): {e}")

    def save_state(self):
        """Guarda el estado actual en disco de forma atómica (tmp + os.replace)."""
        try:
            state_dir = os.path.dirname(STATE_CACHE_PATH)
            os.makedirs(state_dir, exist_ok=True)
            self.updated_at = time.time()
            data = {
                "current_conversation_id": self.current_conversation_id,
                "current_conversation_title": self.current_conversation_title,
                "current_model": self.current_model,
                "current_effort": self.current_effort,
                "current_cwd": self.current_cwd,
                "dnd_mode": self.dnd_mode,
                "updated_at": self.updated_at,
            }
            fd, tmp_path = tempfile.mkstemp(dir=state_dir, prefix=".bot_state_", suffix=".tmp")
            try:
                with os.fdopen(fd, "w", encoding="utf-8") as f:
                    json.dump(data, f, indent=2, ensure_ascii=False)
                os.replace(tmp_path, STATE_CACHE_PATH)
            except Exception:
                if os.path.exists(tmp_path):
                    os.remove(tmp_path)
                raise
        except Exception as e:
            logger.error(f"Error guardando estado del bot en {STATE_CACHE_PATH}: {e}")

    def _load_usage(self):
        if os.path.exists(USAGE_CACHE_PATH):
            try:
                with open(USAGE_CACHE_PATH, "r", encoding="utf-8") as f:
                    self.last_usage_data = json.load(f)
            except Exception:
                self.last_usage_data = {}

    def save_usage_cache(self, usage: dict):
        if not isinstance(usage, dict):
            return
        self.last_usage_data = usage
        try:
            state_dir = os.path.dirname(USAGE_CACHE_PATH)
            os.makedirs(state_dir, exist_ok=True)
            with open(USAGE_CACHE_PATH, "w", encoding="utf-8") as f:
                json.dump(usage, f, indent=2)
        except Exception as e:
            logger.error(f"Error guardando cache de uso: {e}")

    def set_cwd(self, path: str) -> bool:
        real_path = os.path.abspath(os.path.expanduser(path))
        if not os.path.isdir(real_path):
            return False
        self.current_cwd = real_path
        self.save_state()
        return True

    def set_model(self, model_id: Optional[str]):
        self.current_model = model_id if (model_id and model_id != "DEFAULT") else None
        self.save_state()

    def set_effort(self, effort_level: str) -> bool:
        clean = effort_level.strip().lower()
        if clean not in VALID_EFFORTS:
            return False
        self.current_effort = clean
        self.save_state()
        return True

    def set_dnd_mode(self, mode: str) -> bool:
        clean = mode.strip().lower()
        if clean not in VALID_DND_MODES:
            return False
        self.dnd_mode = clean
        self.save_state()
        return True

    def set_conversation(self, conv_id: Optional[str], title: str):
        self.current_conversation_id = conv_id
        self.current_conversation_title = title
        self.save_state()

    def get_cwd_short_name(self, cwd_path: Optional[str] = None) -> str:
        target = cwd_path or self.current_cwd
        if os.path.abspath(target) == os.path.abspath(HOME_DIR):
            return "~ (/home/paulobot)"
        return os.path.basename(target.rstrip("/")) or target


session_state = BotSessionState()
