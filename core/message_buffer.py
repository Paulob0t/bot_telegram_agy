"""
Agrupador de mensajes largos.

Telegram parte los textos que superan 4096 caracteres en varios mensajes
consecutivos. Sin este buffer, cada fragmento se enviaría a AGY como un prompt
independiente. Cuando llega un mensaje mayor a LONG_MESSAGE_THRESHOLD, se abre
una ventana de LONG_MESSAGE_WAIT_SECONDS: todo texto que llegue en ese lapso
(incluido el último fragmento, aunque sea corto) se une y se envía como un solo
prompt. Cada fragmento nuevo reinicia la ventana.
"""
import asyncio
from dataclasses import dataclass, field
from typing import Any, Awaitable, Callable, Dict, List, Optional

from core.config import LONG_MESSAGE_THRESHOLD, LONG_MESSAGE_WAIT_SECONDS, logger


@dataclass
class _PendingBuffer:
    parts: List[str] = field(default_factory=list)
    first_message: Any = None
    timer: Optional[asyncio.Task] = None


class LongMessageBuffer:
    def __init__(
        self,
        threshold: int = LONG_MESSAGE_THRESHOLD,
        wait_seconds: float = LONG_MESSAGE_WAIT_SECONDS,
    ):
        self.threshold = threshold
        self.wait_seconds = wait_seconds
        self._pending: Dict[int, _PendingBuffer] = {}
        self._lock = asyncio.Lock()

    def is_long(self, text: str) -> bool:
        return len(text) > self.threshold

    async def handle(
        self,
        chat_id: int,
        text: str,
        message_obj: Any,
        submit: Callable[[str, Any], Awaitable[None]],
    ) -> bool:
        """
        Retorna True si el texto quedó en buffer (el caller no debe enviarlo).
        Retorna False si es un mensaje corto normal que debe enviarse ya.
        """
        async with self._lock:
            buf = self._pending.get(chat_id)
            if buf is None:
                if not self.is_long(text):
                    return False
                buf = _PendingBuffer(first_message=message_obj)
                self._pending[chat_id] = buf
                try:
                    await message_obj.reply_text(
                        f"📥 Mensaje largo ({len(text)} chars). "
                        f"Esperando {self.wait_seconds:g}s por más partes..."
                    )
                except Exception:
                    pass

            buf.parts.append(text)
            if buf.timer and not buf.timer.done():
                buf.timer.cancel()
            buf.timer = asyncio.create_task(self._flush_later(chat_id, submit))
            return True

    async def _flush_later(self, chat_id: int, submit: Callable[[str, Any], Awaitable[None]]):
        try:
            await asyncio.sleep(self.wait_seconds)
        except asyncio.CancelledError:
            return
        async with self._lock:
            buf = self._pending.pop(chat_id, None)
        if not buf or not buf.parts:
            return
        prompt = "\n".join(buf.parts)
        logger.info("Mensaje largo unido: %d partes, %d chars", len(buf.parts), len(prompt))
        if len(buf.parts) > 1:
            try:
                await buf.first_message.reply_text(
                    f"🧩 {len(buf.parts)} partes unidas ({len(prompt)} chars). Enviando a AGY."
                )
            except Exception:
                pass
        await submit(prompt, buf.first_message)


long_message_buffer = LongMessageBuffer()
