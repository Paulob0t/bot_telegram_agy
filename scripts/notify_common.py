"""
Utilidades compartidas de notificación (Telegram + Omarchy Desktop) para los scripts
`agy-hook-notify`, `claude-hook-notify` y `notify-telegram`.
0 dependencias externas, 0 consumo de tokens.
"""
import os
import re
import shutil
import subprocess
import sys
import urllib.parse
import urllib.request

TITLE_MAX = 50
TELEGRAM_BODY_MAX = 3500


def load_env():
    for p in (
        "/run/media/paulobot/Datos/Projects/telegram-paulobot/.env",
        os.path.expanduser("~/.config/telegram-bot/.env"),
    ):
        if os.path.isfile(p):
            try:
                with open(p, "r", encoding="utf-8") as f:
                    for raw in f:
                        line = raw.strip()
                        if line and not line.startswith("#") and "=" in line:
                            k, v = line.split("=", 1)
                            os.environ.setdefault(k.strip(), v.strip().strip('"').strip("'"))
            except Exception:
                pass


load_env()
TOKEN = os.environ.get("TELEGRAM_BOT_TOKEN", "").strip()
CHAT_ID = os.environ.get("TELEGRAM_CHAT_ID", os.environ.get("TELEGRAM_AUTHORIZED_USER_ID", "")).strip()


def clip(text: str, limit: int) -> str:
    if len(text) <= limit:
        return text
    cut = text[:limit].rsplit(" ", 1)[0]
    return cut.rstrip(" ,;:.") + "…"


def shorten_title(user_task: str) -> str:
    text = re.sub(r"<[^>]+>", "", user_task or "").strip()
    first = text.split("\n")[0].strip() if text else ""
    return clip(first, TITLE_MAX)


def format_full_response(response: str) -> str:
    """Devuelve el desglose exacto del modelo convirtiendo links file:// a código legible."""
    if not response:
        return ""
    text = response.strip()
    text = re.sub(r"\[`?([^\]`]+)`?\]\(file://[^\)]+\)", r"`\1`", text)
    text = re.sub(r"\[([^\]]+)\]\(conversation://[^\)]+\)", r"`\1`", text)
    return clip(text, TELEGRAM_BODY_MAX)


def send_desktop_check(app_name: str, ws_name: str):
    """Muestra únicamente `✓ Tarea lista · <Proyecto>` en Omarchy."""
    title = f"Tarea lista · {ws_name}" if ws_name else "Tarea lista"
    if shutil.which("omarchy-notification-send"):
        subprocess.run(
            ["omarchy-notification-send", "--app-name", app_name, "-g", "✓", "-u", "normal", "-t", "4000", title],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
    elif shutil.which("notify-send"):
        subprocess.run(
            ["notify-send", "-a", app_name, "-u", "normal", f"✓ {title}"],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )


def send_telegram(text: str, parse_mode: str = "Markdown", timeout: int = 5) -> bool:
    if not TOKEN or not CHAT_ID:
        return False
    url = f"https://api.telegram.org/bot{TOKEN}/sendMessage"
    payload = {
        "chat_id": CHAT_ID,
        "text": text[:4000],
        "parse_mode": parse_mode,
    }
    try:
        data = urllib.parse.urlencode(payload).encode("utf-8")
        req = urllib.request.Request(url, data=data)
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.status == 200
    except Exception:
        try:
            payload.pop("parse_mode", None)
            data = urllib.parse.urlencode(payload).encode("utf-8")
            req = urllib.request.Request(url, data=data)
            with urllib.request.urlopen(req, timeout=timeout) as resp:
                return resp.status == 200
        except Exception as err:
            print(f"Error enviando mensaje a Telegram: {err}", file=sys.stderr)
            return False
