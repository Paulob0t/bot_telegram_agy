"""
Guardia de contexto de chat.

Problema: tras un rato sin usar el bot se olvida en qué conversación de AGY
quedó y un mensaje nuevo termina mezclado con un chat que no tiene nada que ver.

Solución: si pasaron más de CHAT_IDLE_MINUTES desde el último prompt enviado,
el mensaje NO se ejecuta directo. Se retiene y se pregunta con botones:
  ➡️ Continuar en «título actual»   |   🆕 Nuevo chat   |   📜 Elegir otro
"""
import time
import uuid
from dataclasses import dataclass
from typing import Any, Awaitable, Callable, Dict, Optional

from telegram import InlineKeyboardButton, InlineKeyboardMarkup

from core.config import CHAT_IDLE_MINUTES, logger
from core.state import session_state
from core.utils import safe_reply


@dataclass
class _HeldPrompt:
    prompt: str
    message_obj: Any
    created_at: float


class ChatGuard:
    def __init__(self, idle_minutes: float = CHAT_IDLE_MINUTES):
        self.idle_seconds = idle_minutes * 60
        # None = bot recién iniciado: el primer mensaje siempre confirma chat
        self.last_prompt_at: Optional[float] = None
        self._held: Dict[str, _HeldPrompt] = {}

    def touch(self):
        self.last_prompt_at = time.time()

    def needs_confirmation(self) -> bool:
        if self.idle_seconds <= 0:
            return False
        # Chat nuevo explícito: no hay riesgo de mezclar
        if session_state.current_conversation_id == "NEW":
            return False
        if self.last_prompt_at is None:
            return True
        return (time.time() - self.last_prompt_at) > self.idle_seconds

    def _idle_text(self) -> str:
        if self.last_prompt_at is None:
            return "el bot se reinició"
        mins = int((time.time() - self.last_prompt_at) // 60)
        if mins >= 60:
            return f"pasaron {mins // 60}h {mins % 60}min"
        return f"pasaron {mins} min"

    async def handle(
        self,
        prompt: str,
        message_obj: Any,
        submit: Callable[[str, Any], Awaitable[None]],
    ) -> None:
        if not self.needs_confirmation():
            self.touch()
            await submit(prompt, message_obj)
            return

        hold_id = uuid.uuid4().hex[:8]
        # Limpiar retenidos viejos (> 1 h) para no acumular memoria
        now = time.time()
        for k in [k for k, v in self._held.items() if now - v.created_at > 3600]:
            self._held.pop(k, None)
        self._held[hold_id] = _HeldPrompt(prompt, message_obj, now)

        title = session_state.current_conversation_title or "Último chat activo"
        preview = prompt.replace("\n", " ")
        if len(preview) > 80:
            preview = preview[:80] + "..."

        keyboard = InlineKeyboardMarkup([
            [InlineKeyboardButton(f"➡️ Continuar: {title[:40]}", callback_data=f"cg_cont:{hold_id}")],
            [
                InlineKeyboardButton("🆕 Nuevo chat", callback_data=f"cg_new:{hold_id}"),
                InlineKeyboardButton("🗑️ Descartar", callback_data=f"cg_drop:{hold_id}"),
            ],
        ])
        await safe_reply(
            message_obj,
            f"🧭 **¿En qué chat va esto?** ({self._idle_text()} desde tu último mensaje)\n\n"
            f"Chat actual: **{title}**\n"
            f"Mensaje: `{preview}`\n\n"
            f"_Para otro chat existente usa /resume y reenvía el mensaje._",
            reply_markup=keyboard,
        )

    async def resolve(
        self,
        action: str,
        hold_id: str,
        submit: Callable[[str, Any], Awaitable[None]],
    ) -> Optional[str]:
        """Aplica la elección del usuario. Retorna texto de confirmación o None si expiró."""
        held = self._held.pop(hold_id, None)
        if held is None:
            return None
        if action == "drop":
            return "🗑️ Mensaje descartado."
        if action == "new":
            session_state.set_conversation("NEW", "➕ Nuevo chat independiente")
            label = "🆕 Nuevo chat"
        else:
            label = f"➡️ {session_state.current_conversation_title}"
        logger.info("ChatGuard: %s (%s)", action, hold_id)
        self.touch()
        await submit(held.prompt, held.message_obj)
        return f"✅ Enviado a **{label}**"


chat_guard = ChatGuard()
