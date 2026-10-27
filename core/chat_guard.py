"""
Guardia de contexto de chat.

Problema: tras un rato sin usar el bot se olvida en qué conversación de AGY
quedó y un mensaje nuevo termina mezclado con un chat que no tiene nada que ver.

Solución: si pasaron más de CHAT_IDLE_MINUTES desde el último prompt enviado,
el mensaje NO se ejecuta directo. Se retiene y se pregunta con botones:
  ➡️ Continuar en «título actual»   |   🆕 Nuevo chat   |   📂 Elegir otro chat
"""
import math
import time
import uuid
from dataclasses import dataclass, field
from typing import Any, Awaitable, Callable, Dict, List, Optional, Tuple

from telegram import InlineKeyboardButton, InlineKeyboardMarkup

from core.config import CHAT_IDLE_MINUTES, logger
from core.state import session_state
from core.utils import safe_reply
from core.chats_db import get_all_conversations, get_conversation_title


@dataclass
class _HeldPrompt:
    prompt: str
    message_obj: Any
    created_at: float
    temp_files: List[str] = field(default_factory=list)


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

    def _cleanup_expired(self):
        import os
        now = time.time()
        for k in [k for k, v in self._held.items() if now - v.created_at > 3600]:
            old = self._held.pop(k, None)
            if old:
                for tf in old.temp_files:
                    if tf and os.path.exists(tf):
                        try:
                            os.remove(tf)
                        except Exception:
                            pass

    def build_prompt_view(self, hold_id: str) -> Optional[Tuple[InlineKeyboardMarkup, str]]:
        held = self._held.get(hold_id)
        if not held:
            return None

        title = session_state.current_conversation_title or "Último chat activo"
        preview = held.prompt.replace("\n", " ")
        if len(preview) > 80:
            preview = preview[:80] + "..."

        keyboard = InlineKeyboardMarkup([
            [InlineKeyboardButton(f"➡️ Continuar: {title[:38]}", callback_data=f"cg_cont:{hold_id}")],
            [
                InlineKeyboardButton("🆕 Nuevo chat", callback_data=f"cg_new:{hold_id}"),
                InlineKeyboardButton("📂 Elegir otro chat", callback_data=f"cg_pick:{hold_id}:0"),
            ],
            [
                InlineKeyboardButton("🗑️ Descartar mensaje", callback_data=f"cg_drop:{hold_id}"),
            ],
        ])
        text = (
            f"🧭 **¿En qué chat va esto?** ({self._idle_text()} desde tu último mensaje)\n\n"
            f"Chat actual: **{title}**\n"
            f"Mensaje: `{preview}`"
        )
        return keyboard, text

    def build_picker_view(self, hold_id: str, page: int = 0, page_size: int = 5) -> Optional[Tuple[InlineKeyboardMarkup, str]]:
        held = self._held.get(hold_id)
        if not held:
            return None

        all_chats = get_all_conversations()
        total_chats = len(all_chats)
        if total_chats == 0:
            return None

        total_pages = max(1, math.ceil(total_chats / page_size))
        page = max(0, min(page, total_pages - 1))
        current_slice = all_chats[page * page_size : (page + 1) * page_size]

        keyboard = []
        for idx, (_, title, _) in enumerate(current_slice):
            abs_idx = page * page_size + idx
            clean_title = title.strip().replace("\n", " ")
            btn_text = f"💬 {clean_title[:32]}..." if len(clean_title) > 32 else f"💬 {clean_title}"
            keyboard.append([InlineKeyboardButton(btn_text, callback_data=f"cg_sel:{hold_id}:{abs_idx}")])

        nav_row = []
        if page > 0:
            nav_row.append(InlineKeyboardButton("◀️ Anterior", callback_data=f"cg_pick:{hold_id}:{page - 1}"))
        nav_row.append(InlineKeyboardButton(f"📄 {page + 1}/{total_pages}", callback_data="res_noop"))
        if page < total_pages - 1:
            nav_row.append(InlineKeyboardButton("Siguiente ▶️", callback_data=f"cg_pick:{hold_id}:{page + 1}"))
        keyboard.append(nav_row)

        keyboard.append([
            InlineKeyboardButton("🆕 Nuevo chat", callback_data=f"cg_new:{hold_id}"),
            InlineKeyboardButton("🔙 Volver", callback_data=f"cg_back:{hold_id}"),
            InlineKeyboardButton("🗑️ Descartar", callback_data=f"cg_drop:{hold_id}"),
        ])

        preview = held.prompt.replace("\n", " ")
        if len(preview) > 70:
            preview = preview[:70] + "..."

        text = (
            f"📂 **Elige en qué chat enviar tu mensaje** (Pág. {page + 1}/{total_pages})\n"
            f"Mensaje retenido: `{preview}`"
        )
        return InlineKeyboardMarkup(keyboard), text

    async def handle(
        self,
        prompt: str,
        message_obj: Any,
        submit: Callable[..., Awaitable[None]],
        temp_files: Optional[List[str]] = None,
    ) -> None:
        if not self.needs_confirmation():
            self.touch()
            await submit(prompt, message_obj, temp_files=temp_files)
            return

        hold_id = uuid.uuid4().hex[:8]
        self._cleanup_expired()
        self._held[hold_id] = _HeldPrompt(
            prompt=prompt,
            message_obj=message_obj,
            created_at=time.time(),
            temp_files=list(temp_files) if temp_files else [],
        )

        view = self.build_prompt_view(hold_id)
        if view:
            keyboard, text = view
            await safe_reply(message_obj, text, reply_markup=keyboard)

    async def resolve(
        self,
        action: str,
        hold_id: str,
        submit: Callable[..., Awaitable[None]],
        chat_idx: Optional[int] = None,
    ) -> Optional[str]:
        """Aplica la elección del usuario. Retorna texto de confirmación o None si expiró."""
        import os
        held = self._held.pop(hold_id, None)
        if held is None:
            return None
        if action == "drop":
            for tf in held.temp_files:
                if tf and os.path.exists(tf):
                    try:
                        os.remove(tf)
                    except Exception:
                        pass
            return "🗑️ Mensaje descartado."
        if action == "new":
            session_state.set_conversation("NEW", "➕ Nuevo chat independiente")
            label = "🆕 Nuevo chat"
        elif action == "sel" and chat_idx is not None:
            all_chats = get_all_conversations()
            if 0 <= chat_idx < len(all_chats):
                cid, ctitle, _ = all_chats[chat_idx]
                session_state.set_conversation(cid, f"💬 {ctitle}")
                label = f"💬 {ctitle}"
            else:
                label = f"➡️ {session_state.current_conversation_title}"
        else:
            label = f"➡️ {session_state.current_conversation_title}"

        logger.info("ChatGuard: %s (%s)", action, hold_id)
        self.touch()
        await submit(held.prompt, held.message_obj, temp_files=held.temp_files)
        return f"✅ Enviado a **{label}**"


chat_guard = ChatGuard()
