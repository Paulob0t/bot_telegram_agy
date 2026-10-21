import asyncio
import os
import sys

os.environ.setdefault("TELEGRAM_BOT_TOKEN", "test")
os.environ.setdefault("TELEGRAM_AUTHORIZED_USER_ID", "1")
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from core.message_buffer import LongMessageBuffer  # noqa: E402


class FakeMsg:
    def __init__(self):
        self.replies = []

    async def reply_text(self, text, **kw):
        self.replies.append(text)


def run(coro):
    return asyncio.run(coro)


def test_short_message_not_buffered():
    async def go():
        buf = LongMessageBuffer(threshold=400, wait_seconds=0.05)
        sent = []

        async def submit(p, m):
            sent.append(p)

        return await buf.handle(1, "hola", FakeMsg(), submit), sent

    buffered, sent = run(go())
    assert buffered is False and sent == []


def test_long_message_parts_are_joined():
    async def go():
        buf = LongMessageBuffer(threshold=400, wait_seconds=0.1)
        sent = []

        async def submit(p, m):
            sent.append(p)

        msg = FakeMsg()
        assert await buf.handle(1, "a" * 500, msg, submit)
        assert await buf.handle(1, "b" * 4096, FakeMsg(), submit)
        assert await buf.handle(1, "fin", FakeMsg(), submit)  # último fragmento corto
        await asyncio.sleep(0.3)
        return sent

    sent = run(go())
    assert sent == ["a" * 500 + "\n" + "b" * 4096 + "\nfin"]


def test_single_long_message_sent_after_wait():
    async def go():
        buf = LongMessageBuffer(threshold=400, wait_seconds=0.05)
        sent = []

        async def submit(p, m):
            sent.append(p)

        await buf.handle(1, "x" * 401, FakeMsg(), submit)
        await asyncio.sleep(0.2)
        # buffer liberado: siguiente corto pasa directo
        nxt = await buf.handle(1, "corto", FakeMsg(), submit)
        return sent, nxt

    sent, nxt = run(go())
    assert sent == ["x" * 401] and nxt is False


def test_exact_threshold_is_not_long():
    assert LongMessageBuffer(threshold=400).is_long("a" * 400) is False
