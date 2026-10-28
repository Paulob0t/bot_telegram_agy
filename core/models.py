import os
import re
import json
import time
import asyncio
from datetime import datetime
from typing import List, Tuple, Optional, Dict
from telegram import InlineKeyboardButton, InlineKeyboardMarkup
from core.config import AGY_BIN, BASE_CLI_DIR, logger
from core.state import session_state

AI_TOKEN_STATS_BIN = "/home/paulobot/.local/bin/ai-token-stats"

MODELS_CACHE_PATH = os.path.join(BASE_CLI_DIR, "telegram_models_cache.json")
AGY_SETTINGS_PATH = os.path.join(BASE_CLI_DIR, "settings.json")

# Catálogo base actualizado con los modelos reales de AGY CLI
DEFAULT_MODELS_CATALOG: List[Dict[str, str]] = [
    {"id": "gemini-4-argon-medium", "name": "Gemini 4 Argon (Medium)", "icon": "🧪"},
    {"id": "gemini-4-argon-high", "name": "Gemini 4 Argon (High)", "icon": "🧪"},
    {"id": "gemini-4-argon-low", "name": "Gemini 4 Argon (Low)", "icon": "🧪"},
    {"id": "gemini-3.8-flash-high", "name": "Gemini 3.8 Flash (High)", "icon": "⚡"},
    {"id": "gemini-3.8-flash-medium", "name": "Gemini 3.8 Flash (Medium)", "icon": "⚡"},
    {"id": "gemini-3.8-flash-low", "name": "Gemini 3.8 Flash (Low)", "icon": "⚡"},
    {"id": "claude-opus-5-5-high", "name": "Claude Opus 5.5 (High)", "icon": "👑"},
    {"id": "claude-opus-5-5-medium", "name": "Claude Opus 5.5 (Medium)", "icon": "👑"},
    {"id": "claude-opus-5-5-low", "name": "Claude Opus 5.5 (Low)", "icon": "👑"},
    {"id": "claude-sonnet-5-5-high", "name": "Claude Sonnet 5.5 (High)", "icon": "🎯"},
    {"id": "claude-sonnet-5-5-medium", "name": "Claude Sonnet 5.5 (Medium)", "icon": "🎯"},
    {"id": "claude-sonnet-5-5-low", "name": "Claude Sonnet 5.5 (Low)", "icon": "🎯"},
    {"id": "gpt-oss-120b-medium", "name": "GPT-OSS 120B (Medium)", "icon": "🤖"},
]

ANSI_ESCAPE_RE = re.compile(r"\x1B(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~])")


def _pick_icon_for_model(model_id: str) -> str:
    m = model_id.lower()
    if "deepseek" in m:
        return "🧠" if "reason" in m else "🐋"
    if "argon" in m:
        return "🧪"
    if "flash" in m:
        return "⚡"
    if "pro" in m:
        return "🧠"
    if "opus" in m:
        return "👑"
    if "sonnet" in m or "claude" in m:
        return "🎯"
    if "gpt" in m:
        return "🤖"
    return "✨"


def get_context_window_for_model(model_identifier: str) -> int:
    """Retorna el tamaño real de ventana de contexto en tokens según la familia del modelo."""
    m = (model_identifier or "").lower()
    if "deepseek" in m:
        return 64_000
    if any(k in m for k in ("claude", "sonnet", "opus")):
        return 200_000
    if "gpt-oss" in m or "gpt" in m:
        return 128_000
    # Gemini 4 Argon / Gemini 3.8 Flash / Gemini Pro tienen 1M (1,048,576) tokens
    return 1_048_576


def get_cli_default_model() -> str:
    """Lee ~/.gemini/antigravity-cli/settings.json para obtener el modelo por defecto real de AGY."""
    if os.path.exists(AGY_SETTINGS_PATH):
        try:
            with open(AGY_SETTINGS_PATH, "r", encoding="utf-8") as f:
                data = json.load(f)
            mod = data.get("model")
            if mod and isinstance(mod, str):
                return mod.strip()
        except Exception:
            pass
    return "Gemini 4 Argon (Medium)"


def get_effective_model_display() -> Tuple[str, int]:
    """
    Devuelve (nombre_efectivo_del_modelo, max_context_tokens) considerando
    tanto el override del bot como el default configurado en settings.json de AGY.
    """
    cli_default = get_cli_default_model()
    if session_state.current_model:
        active_id = session_state.current_model
        # Buscar nombre amigable en el catálogo
        catalog = get_models_catalog()
        friendly = next((item["name"] for item in catalog if item["id"] == active_id), active_id)
        return f"{friendly} (`{active_id}`)", get_context_window_for_model(active_id)
    else:
        return f"Por defecto CLI ({cli_default})", get_context_window_for_model(cli_default)


def get_models_catalog() -> List[Dict[str, str]]:
    """Obtiene la lista de modelos desde la caché sincronizada o el catálogo base."""
    if os.path.exists(MODELS_CACHE_PATH):
        try:
            with open(MODELS_CACHE_PATH, "r", encoding="utf-8") as f:
                data = json.load(f)
            models = data.get("models", [])
            if isinstance(models, list) and len(models) > 0:
                return models
        except Exception:
            pass
    return DEFAULT_MODELS_CATALOG


async def sync_models_from_cli(timeout: float = 15.0) -> List[Dict[str, str]]:
    """
    Ejecuta `agy models` de forma asíncrona, parsea la salida limpia de modelos
    disponibles y actualiza la caché local en disco.
    """
    env = os.environ.copy()
    env["PATH"] = f"/home/paulobot/.local/bin:/home/paulobot/.gemini/antigravity-cli/bin:{env.get('PATH', '')}"

    proc = await asyncio.create_subprocess_exec(
        AGY_BIN,
        "models",
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.STDOUT,
        env=env,
    )

    try:
        stdout_bytes, _ = await asyncio.wait_for(proc.communicate(), timeout=timeout)
    except asyncio.TimeoutError:
        try:
            proc.kill()
            await proc.wait()
        except Exception:
            pass
        raise TimeoutError("Tiempo agotado consultando `agy models`.")

    raw_text = stdout_bytes.decode("utf-8", errors="replace")
    clean_text = ANSI_ESCAPE_RE.sub("", raw_text)

    parsed_models: List[Dict[str, str]] = []
    seen_ids = set()

    for raw_line in clean_text.splitlines():
        # Limpiar retornos de carro de los spinners ("Fetching available models...")
        segments = [seg.strip() for seg in raw_line.split("\r") if seg.strip()]
        if not segments:
            continue
        line = segments[-1]
        if "fetching available models" in line.lower():
            continue

        # Formato esperado: "<model-id>     <Display Name>"
        match = re.match(r"^([a-zA-Z0-9\.\-_]+)\s+(.+)$", line)
        if match:
            mod_id = match.group(1).strip()
            mod_name = match.group(2).strip()
            if mod_id not in seen_ids:
                seen_ids.add(mod_id)
                parsed_models.append({
                    "id": mod_id,
                    "name": mod_name,
                    "icon": _pick_icon_for_model(mod_id),
                })

    if parsed_models:
        try:
            os.makedirs(os.path.dirname(MODELS_CACHE_PATH), exist_ok=True)
            with open(MODELS_CACHE_PATH, "w", encoding="utf-8") as f:
                json.dump(
                    {"updated_at": time.time(), "models": parsed_models},
                    f,
                    indent=2,
                    ensure_ascii=False,
                )
            logger.info(f"Catálogo de modelos sincronizado con AGY CLI ({len(parsed_models)} modelos).")
        except Exception as e:
            logger.error(f"Error guardando caché de modelos: {e}")
        return parsed_models

    return get_models_catalog()


def build_models_keyboard() -> Tuple[InlineKeyboardMarkup, str]:
    """Construye el teclado inline con todos los modelos actuales (2 por fila o 1 por fila agrupado)."""
    catalog = get_models_catalog()
    cli_default = get_cli_default_model()
    active_mod = session_state.current_model or "DEFAULT"

    keyboard: List[List[InlineKeyboardButton]] = []

    # Opción por defecto de AGY CLI arriba del todo
    default_prefix = "✅ " if active_mod == "DEFAULT" else "🔄 "
    keyboard.append([
        InlineKeyboardButton(
            f"{default_prefix}Default CLI: {cli_default}",
            callback_data="mod:DEFAULT",
        )
    ])

    # Modelos del catálogo (1 por fila si son <= 8, o 2 por fila para que sea compacto en móvil)
    row: List[InlineKeyboardButton] = []
    for item in catalog:
        mod_id = item["id"]
        mod_name = item["name"]
        icon = item.get("icon", "✨")
        is_selected = mod_id == active_mod
        prefix = "✅ " if is_selected else f"{icon} "

        # Etiqueta compacta para 2 columnas en móvil
        short_label = (
            mod_name.replace("Gemini ", "G-")
            .replace("Claude ", "")
            .replace("(Medium)", "(Med)")
        )
        row.append(InlineKeyboardButton(f"{prefix}{short_label}", callback_data=f"mod:{mod_id}"))
        if len(row) == 2:
            keyboard.append(row)
            row = []

    if row:
        keyboard.append(row)

    # Botón para sincronizar dinámicamente con `agy models`
    keyboard.append([
        InlineKeyboardButton("🔄 Sincronizar lista con `agy models`", callback_data="mod_sync")
    ])

    eff_disp, ctx_win = get_effective_model_display()
    text = (
        "🤖 **Catálogo Dinámico de Modelos (AGY CLI)**\n\n"
        f"• 📌 **Activo:** {eff_disp}\n"
        f"• 🪟 **Ventana de Contexto:** `{ctx_win:,}` tokens\n"
        f"• ⚙️ **Default en `settings.json`:** `{cli_default}`\n\n"
        "*Toca un modelo para activarlo (se guarda de forma persistente) o sincroniza con el CLI:*"
    )
    return InlineKeyboardMarkup(keyboard), text


def _fmt_iso_local(iso_str: str) -> str:
    if not iso_str:
        return ""
    try:
        dt = datetime.fromisoformat(iso_str.replace("Z", "+00:00")).astimezone()
        dias = ["lun", "mar", "mié", "jue", "vie", "sáb", "dom"]
        h = dt.hour % 12 or 12
        ampm = "AM" if dt.hour < 12 else "PM"
        return f"{dias[dt.weekday()]} {h}:{dt.minute:02d} {ampm}"
    except Exception:
        return iso_str


def _fmt_claude_reset(raw: str) -> str:
    return re.sub(r"\s*\(.*\)\s*$", "", str(raw or "")).strip()


def _quota_bar(pct: float, is_used: bool = False, width: int = 12) -> str:
    val = max(0.0, min(100.0, float(pct)))
    filled = int(round((val / 100.0) * width))
    bar = "█" * filled + "░" * (width - filled)
    critical = (val >= 85) if is_used else (val <= 10)
    tag = "usado" if is_used else "libre"
    pct_str = f"{val:g}%"
    alert = " ⚠️" if critical else ""
    return f"`[{bar}]` **{pct_str}** {tag}{alert}"


async def build_ai_quotas_dashboard() -> Tuple[InlineKeyboardMarkup, str]:
    """
    Ejecuta `/home/paulobot/.local/bin/ai-token-stats` (mismo backend del widget
    Quickshell `paulobot.ai-tokens`) y construye el panel de cuotas de Claude Code y Antigravity.
    """
    env = os.environ.copy()
    env["PATH"] = f"/home/paulobot/.local/bin:/home/paulobot/.gemini/antigravity-cli/bin:{env.get('PATH', '')}"

    data = {}
    try:
        proc = await asyncio.create_subprocess_exec(
            AI_TOKEN_STATS_BIN,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.DEVNULL,
            env=env,
        )
        stdout_bytes, _ = await asyncio.wait_for(proc.communicate(), timeout=18.0)
        data = json.loads(stdout_bytes.decode("utf-8", errors="replace").strip() or "{}")
    except Exception as e:
        logger.warning("Error ejecutando ai-token-stats: %s", e)

    lines = ["📊 **Uso de IA (Quickshell Sync)**\n"]

    # 1. Claude Code (porcentaje USADO, igual que en BarWidget.qml)
    c = data.get("claude_code") or {}
    lines.append("🟠 **CLAUDE CODE**")
    if c.get("available"):
        s_used = c.get("session_used_pct", 0)
        w_used = c.get("week_used_pct", 0)
        s_rst = _fmt_claude_reset(c.get("session_resets", ""))
        w_rst = _fmt_claude_reset(c.get("week_resets", ""))
        lines.append(f"• **Sesión · 5 h:** {_quota_bar(s_used, is_used=True)}" + (f" _(Reinicia {s_rst})_" if s_rst else ""))
        lines.append(f"• **Semanal:** {_quota_bar(w_used, is_used=True)}" + (f" _(Reinicia {w_rst})_" if w_rst else ""))
    else:
        lines.append("• _No disponible_")

    lines.append("")

    # 2. Antigravity (porcentaje LIBRE/RESTANTE, igual que en BarWidget.qml)
    ag = data.get("antigravity") or {}
    lines.append("🔵 **ANTIGRAVITY**")
    if ag.get("available"):
        gem = ag.get("gemini") or {}
        cg = ag.get("claude_gpt") or {}
        arg = ag.get("argon") or {}

        g5 = gem.get("five_hour_remaining_pct", 0)
        gw = gem.get("week_remaining_pct", 0)
        g5_r = _fmt_iso_local(gem.get("five_hour_reset", ""))
        gw_r = _fmt_iso_local(gem.get("week_reset", ""))

        c5 = cg.get("five_hour_remaining_pct", 0)
        cw = cg.get("week_remaining_pct", 0)
        c5_r = _fmt_iso_local(cg.get("five_hour_reset", ""))
        cw_r = _fmt_iso_local(cg.get("week_reset", ""))

        aw = arg.get("week_remaining_pct", 0)
        aw_r = _fmt_iso_local(arg.get("week_reset", ""))

        lines.append(f"• **Gemini · 5 h:** {_quota_bar(g5)}" + (f" _(Reinicia {g5_r})_" if g5_r else ""))
        lines.append(f"• **Gemini · Semanal:** {_quota_bar(gw)}" + (f" _(Reinicia {gw_r})_" if gw_r else ""))
        lines.append(f"• **Claude & GPT · 5 h:** {_quota_bar(c5)}" + (f" _(Reinicia {c5_r})_" if c5_r else ""))
        lines.append(f"• **Claude & GPT · Semanal:** {_quota_bar(cw)}" + (f" _(Reinicia {cw_r})_" if cw_r else ""))
        lines.append(f"• **Gemini 4 Argon · Semanal:** {_quota_bar(aw)}" + (f" _(Reinicia {aw_r})_" if aw_r else ""))
    else:
        lines.append("• _No disponible_")

    eff_disp, _ = get_effective_model_display()
    lines.append(f"\n🤖 **Modelo activo:** {eff_disp}")

    keyboard = InlineKeyboardMarkup([
        [InlineKeyboardButton("🔄 Actualizar Cuotas", callback_data="usage_sync")]
    ])
    return keyboard, "\n".join(lines)
