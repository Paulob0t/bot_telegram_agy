import os
import json
import time
import sqlite3
import tempfile
from typing import Optional
from core.config import HOME_DIR, DB_PATH, USAGE_CACHE_PATH, STATE_CACHE_PATH, logger

VALID_EFFORTS = ("low", "medium", "high", "xhigh", "max")


class BotSessionState:
    """
    Estado centralizado de la sesión de AGY con persistencia atómica en disco
    (~/.gemini/antigravity-cli/telegram_bot_state.json).
    """

    def __init__(self):
        self.current_engine: str = "agy"  # 'agy' o 'claude'
        self.current_conversation_id: Optional[str] = None  # None = último (-c), 'NEW' = nuevo, o UUID
        self.current_conversation_title: str = "🔄 Continuar último chat activo (-c)"
        self.claude_session_id: Optional[str] = None  # None = último (-c), 'NEW' = nuevo, o UUID de Claude
        self.claude_session_title: str = "🔄 Último chat de Claude (-c)"
        self.claude_model: Optional[str] = None  # None = default, o 'sonnet', 'opus', 'haiku'
        self.current_model: Optional[str] = None  # None = por defecto o ID específico
        self.current_effort: str = "low"  # low, medium, high, xhigh, max
        self.current_cwd: str = HOME_DIR  # Directorio de trabajo activo para AGY
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

            # 2. Restaurar modelo y sincronizar esfuerzo si el modelo ya incluye sufijo
            model = data.get("current_model", None)
            self.current_model = str(model).strip() if model else None
            if self.current_model:
                lower_m = self.current_model.lower()
                for eff in ("low", "medium", "high"):
                    if lower_m.endswith(f"-{eff}"):
                        self.current_effort = eff
                        break

            # 3. Restaurar directorio de trabajo (cwd)
            saved_cwd = data.get("current_cwd", HOME_DIR)
            if saved_cwd and os.path.isdir(saved_cwd):
                self.current_cwd = os.path.abspath(saved_cwd)
            else:
                self.current_cwd = HOME_DIR

            # 4. Restaurar motor ('agy' o 'claude') y sesiones
            engine = str(data.get("current_engine", "agy")).lower()
            self.current_engine = "claude" if engine == "claude" else "agy"
            self.claude_session_id = data.get("claude_session_id", None)
            self.claude_session_title = data.get("claude_session_title", "🔄 Último chat de Claude (-c)")
            c_mod = data.get("claude_model", None)
            self.claude_model = str(c_mod).strip() if c_mod else None

            # 5. Restaurar conversación activa de AGY y sincronizar título con SQLite
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

            self.updated_at = data.get("updated_at", time.time())
            logger.info(
                f"Estado restaurado desde {STATE_CACHE_PATH}: "
                f"cwd='{self.current_cwd}', model='{self.current_model or 'DEFAULT'}', "
                f"effort='{self.current_effort}'"
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
                "current_engine": self.current_engine,
                "current_conversation_id": self.current_conversation_id,
                "current_conversation_title": self.current_conversation_title,
                "claude_session_id": self.claude_session_id,
                "claude_session_title": self.claude_session_title,
                "claude_model": self.claude_model,
                "current_model": self.current_model,
                "current_effort": self.current_effort,
                "current_cwd": self.current_cwd,
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
        if self.current_engine == "claude":
            self.claude_model = model_id if (model_id and model_id != "DEFAULT") else None
            self.save_state()
            return
        self.current_model = model_id if (model_id and model_id != "DEFAULT") else None
        if self.current_model:
            lower_m = self.current_model.lower()
            for eff in ("low", "medium", "high"):
                if lower_m.endswith(f"-{eff}"):
                    self.current_effort = eff
                    break
        self.save_state()

    def set_effort(self, effort_level: str) -> bool:
        clean = effort_level.strip().lower()
        if clean not in VALID_EFFORTS:
            return False
        self.current_effort = clean
        if self.current_model:
            lower_m = self.current_model.lower()
            for suffix in ("-low", "-medium", "-high"):
                if lower_m.endswith(suffix):
                    base = self.current_model[: -len(suffix)]
                    target_suffix = clean if clean in ("low", "medium", "high") else "high"
                    # gpt-oss-120b only exists as medium
                    if "gpt-oss" in base.lower():
                        self.current_model = f"{base}-medium"
                    else:
                        self.current_model = f"{base}-{target_suffix}"
                    break
        self.save_state()
        return True

    def set_conversation(self, conv_id: Optional[str], title: str):
        self.current_engine = "agy"
        self.current_conversation_id = conv_id
        self.current_conversation_title = title
        self.save_state()

    def set_claude_session(self, session_id: Optional[str], title: str, cwd: Optional[str] = None):
        self.current_engine = "claude"
        self.claude_session_id = session_id
        self.claude_session_title = title
        if cwd and os.path.isdir(cwd):
            self.current_cwd = os.path.abspath(cwd)
        self.save_state()

    def set_engine(self, engine: str):
        self.current_engine = "claude" if str(engine).lower() == "claude" else "agy"
        self.save_state()

    def get_cwd_short_name(self, cwd_path: Optional[str] = None) -> str:
        target = cwd_path or self.current_cwd
        if os.path.abspath(target) == os.path.abspath(HOME_DIR):
            return "~ (/home/paulobot)"
        return os.path.basename(target.rstrip("/")) or target


session_state = BotSessionState()
