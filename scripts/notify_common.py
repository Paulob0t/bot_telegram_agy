"""
Utilidades compartidas de notificación (Telegram + Omarchy Desktop) para los scripts
`agy-hook-notify`, `claude-hook-notify` y `notify-telegram`.
0 dependencias externas, 0 consumo de tokens.
"""
import json
import os
import re
import shutil
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request

TITLE_MAX = 50
TELEGRAM_BODY_MAX = 3500
STATE_PATH = os.path.expanduser("~/.gemini/antigravity-cli/telegram_bot_state.json")


def _is_feature_enabled(key: str) -> bool:
    if os.path.isfile(STATE_PATH):
        try:
            with open(STATE_PATH, "r", encoding="utf-8") as f:
                data = json.load(f)
            features = data.get("features")
            if isinstance(features, dict) and key in features:
                return bool(features[key])
        except Exception:
            pass
    return True


def load_env():
    repo_root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    env_file = os.environ.get("BOT_ENV_FILE", ".env")
    for p in (
        os.path.join(repo_root, env_file),
        os.path.join(repo_root, ".env"),
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
    """Muestra aviso nativo de escritorio (Omarchy/Linux o Windows Balloon Toast)."""
    if not _is_feature_enabled("notify_desktop"):
        return
    title = f"Tarea lista · {ws_name}" if ws_name else "Tarea lista"
    if sys.platform == "win32":
        safe_app = app_name.replace("'", "''")
        safe_title = f"✓ {title}".replace("'", "''")
        ps_cmd = (
            "Add-Type -AssemblyName System.Windows.Forms,System.Drawing; "
            "$n = New-Object System.Windows.Forms.NotifyIcon; "
            "$n.Icon = [System.Drawing.SystemIcons]::Information; "
            "$n.Visible = $true; "
            f"$n.ShowBalloonTip(4000, '{safe_app}', '{safe_title}', [System.Windows.Forms.ToolTipIcon]::Info); "
            "Start-Sleep -Milliseconds 4200; "
            "$n.Dispose()"
        )
        subprocess.Popen(
            ["powershell", "-NoProfile", "-WindowStyle", "Hidden", "-Command", ps_cmd],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0x08000000),
        )
        return
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
    if not _is_feature_enabled("notify_telegram"):
        return False
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
    except urllib.error.HTTPError:
        try:
            payload.pop("parse_mode", None)
            data = urllib.parse.urlencode(payload).encode("utf-8")
            req = urllib.request.Request(url, data=data)
            with urllib.request.urlopen(req, timeout=timeout) as resp:
                return resp.status == 200
        except Exception as err:
            print(f"Error enviando mensaje a Telegram: {err}", file=sys.stderr)
            return False
    except Exception as err:
        print(f"Error de red enviando mensaje a Telegram: {err}", file=sys.stderr)
        return False
