#!/usr/bin/env python3
"""
telegram-notify-bridge.py
Escucha las notificaciones de escritorio de Linux (DBus) usando dbus-monitor
y las reenvía de forma asíncrona (no bloqueante) al bot de Telegram.
Soporta Modo No Molestar (/dnd: off | auto | on) sincronizado con telegram_bot_state.json:
  - off:  Reenvía todas las notificaciones.
  - auto: Silencia mientras usas la laptop; reenvía solo cuando la pantalla está bloqueada (hyprlock).
  - on:   Silencia todas las notificaciones de escritorio.
"""

import subprocess
import threading
import queue
import json
import time
import sys
import os
import re
import urllib.request
import urllib.parse

STATE_CACHE_PATH = os.path.expanduser("~/.gemini/antigravity-cli/telegram_bot_state.json")


def _load_env():
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


_load_env()
TOKEN = os.environ.get("TELEGRAM_BOT_TOKEN", "").strip()
CHAT_ID = os.environ.get("TELEGRAM_CHAT_ID", os.environ.get("TELEGRAM_AUTHORIZED_USER_ID", "")).strip()

IGNORE_APPS = {
    "volume", "brightness", "backlight", "osd", "omarchy-osd",
    "puvlo remote", "agy-hook", "puvlo_task_bot", "antigravity", "agy",
}

APP_ICONS = {
    "spotify": "🎵",
    "discord": "💬",
    "telegram": "✈️",
    "steam": "🎮",
    "github": "🐙",
    "slack": "💼",
    "chrome": "🌐",
    "firefox": "🦊",
    "zen": "🌐",
    "ghostty": "💻",
    "kitty": "💻",
    "alacritty": "💻",
    "foot": "💻",
    "terminal": "💻",
    "notify-send": "🔔",
}

recent_cache = {}
send_queue: "queue.Queue[str]" = queue.Queue(maxsize=200)


def get_dnd_mode() -> str:
    """Lee el modo DND actual desde el archivo de estado compartido del bot."""
    if not os.path.exists(STATE_CACHE_PATH):
        return "off"
    try:
        with open(STATE_CACHE_PATH, "r", encoding="utf-8") as f:
            data = json.load(f)
        mode = str(data.get("dnd_mode", "off")).lower()
        return mode if mode in ("off", "auto", "on") else "off"
    except Exception:
        return "off"


def is_screen_locked() -> bool:
    """Verifica si la sesión está bloqueada con hyprlock."""
    try:
        res = subprocess.run(
            ["pgrep", "-x", "hyprlock"],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            timeout=1.0,
        )
        return res.returncode == 0
    except Exception:
        return False


def should_forward_notification() -> bool:
    """Evalúa según el modo /dnd si debe reenviar la notificación al celular."""
    mode = get_dnd_mode()
    if mode == "on":
        return False
    if mode == "auto":
        return is_screen_locked()
    return True


def clean_dbus_string(raw_val: str) -> str:
    """Decodifica escapes octales de dbus-monitor a UTF-8 real sin romper caracteres."""
    if not raw_val:
        return ""
    try:
        b = re.sub(
            rb"\\([0-7]{3})",
            lambda m: bytes([int(m.group(1), 8)]),
            raw_val.encode("utf-8", "surrogateescape"),
        )
        return (
            b.decode("utf-8", errors="replace")
            .replace("\\n", "\n")
            .replace("\\t", "\t")
            .replace('\\"', '"')
            .replace("\\\\", "\\")
            .strip()
        )
    except Exception:
        return raw_val.strip()


def _send_telegram_sync(text: str):
    if not TOKEN or not CHAT_ID:
        return
    url = f"https://api.telegram.org/bot{TOKEN}/sendMessage"
    payload = {
        "chat_id": CHAT_ID,
        "text": text[:4000],
        "parse_mode": "Markdown",
    }
    try:
        data = urllib.parse.urlencode(payload).encode("utf-8")
        req = urllib.request.Request(url, data=data)
        urllib.request.urlopen(req, timeout=5)
    except Exception:
        try:
            payload.pop("parse_mode", None)
            data = urllib.parse.urlencode(payload).encode("utf-8")
            req = urllib.request.Request(url, data=data)
            urllib.request.urlopen(req, timeout=5)
        except Exception as e:
            print(f"Error enviando a Telegram: {e}", file=sys.stderr)


def _sender_worker():
    """Hilo consumidor no bloqueante para enviar mensajes a Telegram."""
    while True:
        msg = send_queue.get()
        if msg is None:
            break
        try:
            _send_telegram_sync(msg)
        except Exception as e:
            print(f"Excepción en worker de envío: {e}", file=sys.stderr)
        finally:
            send_queue.task_done()


def enqueue_telegram(text: str):
    try:
        send_queue.put_nowait(text)
    except queue.Full:
        pass


def format_notification(app_name: str, summary: str, body: str) -> str:
    icon = "🖥️"
    for key, ic in APP_ICONS.items():
        if key in app_name.lower():
            icon = ic
            break

    if summary.lower() == app_name.lower():
        summary = ""

    body = body.replace("✓", "✅").replace("â…", "✅").replace("✗", "❌")
    summary = summary.replace("✓", "✅").replace("â…", "✅").replace("✗", "❌")

    app_label = app_name if app_name and app_name != "notify-send" else "Sistema"

    if summary and body:
        return f"{icon} **[{app_label}]**\n📌 **{summary}**\n\n{body}"
    elif summary:
        return f"{icon} **[{app_label}]**\n📌 **{summary}**"
    elif body:
        return f"{icon} **[{app_label}]**\n\n{body}"
    else:
        return f"{icon} **[{app_label}]**\n📌 *Notificación recibida*"


def parse_dbus_monitor():
    cmd = [
        "stdbuf", "-oL",
        "dbus-monitor",
        "type='method_call',interface='org.freedesktop.Notifications',member='Notify'",
    ]
    proc = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True, bufsize=1)

    in_notify = False
    nesting_depth = 0
    top_level_strings = []

    for raw_line in iter(proc.stdout.readline, ""):
        line = raw_line.strip()
        if not line:
            continue

        if "interface=org.freedesktop.Notifications; member=Notify" in line:
            in_notify = True
            nesting_depth = 0
            top_level_strings = []
            continue

        if not in_notify:
            continue

        # Rastrear si estamos dentro de array [ o dict entry ( para capturar solo los strings de primer nivel
        if line.startswith("array [") or line.startswith("dict entry(") or line.startswith("struct {"):
            nesting_depth += 1
            # Si ya llegamos al array de actions/hints y tenemos al menos los 4 strings principales, procesar
            if len(top_level_strings) >= 4:
                _process_captured_notify(top_level_strings)
                in_notify = False
                top_level_strings = []
            continue

        if line in ("]", ")", "}"):
            nesting_depth = max(0, nesting_depth - 1)
            continue

        if nesting_depth == 0 and line.startswith("string "):
            # Extraer valor entre comillas de string "..."
            m = re.match(r'^string\s+"(.*)"$', line)
            if m:
                top_level_strings.append(clean_dbus_string(m.group(1)))
            else:
                # Si la cadena continúa en la siguiente línea sin cerrar comilla
                raw_part = line[len("string "):].lstrip('"')
                top_level_strings.append(clean_dbus_string(raw_part))

            if len(top_level_strings) >= 4:
                _process_captured_notify(top_level_strings)
                in_notify = False
                top_level_strings = []


def _process_captured_notify(string_list: list):
    """
    Según la especificación org.freedesktop.Notifications.Notify:
      string #0: app_name
      (uint32 replaces_id)
      string #1: app_icon
      string #2: summary
      string #3: body
    """
    if len(string_list) >= 4:
        app_name = string_list[0]
        summary = string_list[2]
        body = string_list[3]
    elif len(string_list) == 3:
        app_name = string_list[0]
        summary = string_list[1]
        body = string_list[2]
    elif len(string_list) == 2:
        app_name = string_list[0]
        summary = string_list[1]
        body = ""
    else:
        return

    if not summary and not body:
        return

    if (
        app_name.lower() in IGNORE_APPS
        or "antigravity" in app_name.lower()
        or "antigravity" in summary.lower()
    ):
        return

    # Verificar modo No Molestar (/dnd: off | auto | on)
    if not should_forward_notification():
        return

    # Anti-spam de duplicados en ventana de 3 segundos
    key = f"{app_name}:{summary}:{body}"
    now = time.time()
    if key in recent_cache and (now - recent_cache[key]) < 3.0:
        return
    recent_cache[key] = now

    for k in list(recent_cache.keys()):
        if now - recent_cache[k] > 60:
            del recent_cache[k]

    tg_msg = format_notification(app_name, summary, body)
    print(f"Reenviando notificación de [{app_name}]: {summary}", flush=True)
    enqueue_telegram(tg_msg)


if __name__ == "__main__":
    print("Iniciando puente asíncrono de notificaciones de PC a Telegram (con soporte /dnd)...", flush=True)
    worker = threading.Thread(target=_sender_worker, daemon=True)
    worker.start()
    try:
        parse_dbus_monitor()
    except KeyboardInterrupt:
        sys.exit(0)
