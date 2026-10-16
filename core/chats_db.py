import os
import math
import shutil
import sqlite3
from telegram import InlineKeyboardButton, InlineKeyboardMarkup
from core.config import DB_PATH, BASE_CLI_DIR, logger
from core.state import session_state


def get_all_conversations():
    """Obtiene todas las conversaciones históricas guardadas en SQLite."""
    if not os.path.exists(DB_PATH):
        return []
    try:
        conn = sqlite3.connect(DB_PATH)
        cur = conn.cursor()
        cur.execute("""
            SELECT conversation_id, title, last_modified_time 
            FROM conversation_summaries 
            WHERE title != '' 
            ORDER BY last_modified_time DESC
        """)
        rows = cur.fetchall()
        conn.close()
        return rows
    except Exception as e:
        logger.error(f"Error consultando base de datos: {e}")
        return []


def get_conversation_title(conv_id: str) -> str:
    if not os.path.exists(DB_PATH) or not conv_id:
        return "Chat seleccionado"
    try:
        conn = sqlite3.connect(DB_PATH)
        cur = conn.cursor()
        cur.execute("SELECT title FROM conversation_summaries WHERE conversation_id = ?", (conv_id,))
        row = cur.fetchone()
        conn.close()
        if row and row[0]:
            return row[0]
    except Exception:
        pass
    return f"Chat ({conv_id[:8]}...)"


def delete_single_chat(conv_id: str) -> bool:
    """Elimina completamente un chat de SQLite y del sistema de archivos."""
    if not conv_id:
        return False

    if os.path.exists(DB_PATH):
        try:
            conn = sqlite3.connect(DB_PATH)
            cur = conn.cursor()
            cur.execute("DELETE FROM conversation_summaries WHERE conversation_id = ?", (conv_id,))
            conn.commit()
            conn.close()
        except Exception as e:
            logger.error(f"Error borrando de SQLite: {e}")

    paths = [
        os.path.join(BASE_CLI_DIR, "brain", conv_id),
        os.path.join(BASE_CLI_DIR, "conversations", conv_id),
        os.path.join(BASE_CLI_DIR, "conversations", f"{conv_id}.jsonl"),
        os.path.join(BASE_CLI_DIR, "conversations", f"{conv_id}.pb"),
        os.path.join(BASE_CLI_DIR, "presence", conv_id),
        os.path.join(BASE_CLI_DIR, "annotations", conv_id),
    ]
    for p in paths:
        try:
            if os.path.isdir(p):
                shutil.rmtree(p, ignore_errors=True)
            elif os.path.isfile(p):
                os.remove(p)
        except Exception:
            pass

    if session_state.current_conversation_id == conv_id:
        session_state.current_conversation_id = None
        session_state.current_conversation_title = "🔄 Continuar último chat activo (-c)"
        session_state.save_state()

    return True


def build_resume_keyboard(page: int = 0, page_size: int = 5):
    all_chats = get_all_conversations()
    total_chats = len(all_chats)

    if total_chats == 0:
        return None, "❌ No se encontraron conversaciones previas guardadas."

    total_pages = max(1, math.ceil(total_chats / page_size))
    page = max(0, min(page, total_pages - 1))

    start_idx = page * page_size
    end_idx = start_idx + page_size
    current_chats = all_chats[start_idx:end_idx]

    keyboard = []
    for cid, title, _ in current_chats:
        clean_title = title.strip().replace("\n", " ")
        btn_text = f"💬 {clean_title[:32]}..." if len(clean_title) > 32 else f"💬 {clean_title}"
        keyboard.append([InlineKeyboardButton(btn_text, callback_data=f"res:{cid}")])

    nav_row = []
    if page > 0:
        nav_row.append(InlineKeyboardButton("◀️ Anterior", callback_data=f"res_page:{page - 1}"))
    nav_row.append(InlineKeyboardButton(f"📄 {page + 1}/{total_pages}", callback_data="res_noop"))
    if page < total_pages - 1:
        nav_row.append(InlineKeyboardButton("Siguiente ▶️", callback_data=f"res_page:{page + 1}"))
    keyboard.append(nav_row)

    keyboard.append([
        InlineKeyboardButton("🔄 Último Activo", callback_data="res:LATEST"),
        InlineKeyboardButton("➕ Nuevo Chat", callback_data="res:NEW"),
        InlineKeyboardButton("🗑️ Borrar Chats", callback_data="del_menu:0"),
    ])

    text = (
        f"🗂️ **Historial Completo de Chats ({total_chats} conversaciones)**\n"
        f"Página **{page + 1} de {total_pages}** • *Toca una para continuarla:*"
    )
    return InlineKeyboardMarkup(keyboard), text


def build_delete_keyboard(page: int = 0, page_size: int = 5):
    all_chats = get_all_conversations()
    total_chats = len(all_chats)

    if total_chats == 0:
        return None, "ℹ️ No hay conversaciones guardadas para eliminar."

    total_pages = max(1, math.ceil(total_chats / page_size))
    page = max(0, min(page, total_pages - 1))

    start_idx = page * page_size
    end_idx = start_idx + page_size
    current_chats = all_chats[start_idx:end_idx]

    keyboard = []
    for cid, title, _ in current_chats:
        clean_title = title.strip().replace("\n", " ")
        btn_text = f"🗑️ {clean_title[:30]}..." if len(clean_title) > 30 else f"🗑️ {clean_title}"
        keyboard.append([InlineKeyboardButton(btn_text, callback_data=f"del_ask:{cid}")])

    nav_row = []
    if page > 0:
        nav_row.append(InlineKeyboardButton("◀️ Anterior", callback_data=f"del_page:{page - 1}"))
    nav_row.append(InlineKeyboardButton(f"📄 {page + 1}/{total_pages}", callback_data="res_noop"))
    if page < total_pages - 1:
        nav_row.append(InlineKeyboardButton("Siguiente ▶️", callback_data=f"del_page:{page + 1}"))
    keyboard.append(nav_row)

    keyboard.append([
        InlineKeyboardButton("🔙 Volver a Chats", callback_data="res_page:0")
    ])

    text = (
        f"🗑️ **Eliminar Conversaciones de AGY**\n"
        f"Página **{page + 1} de {total_pages}** • *Toca un chat para borrarlo permanentemente:*"
    )
    return InlineKeyboardMarkup(keyboard), text
