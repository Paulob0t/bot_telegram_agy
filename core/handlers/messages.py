"""
Handler de mensajes de texto, fotos y archivos de imagen.
Integra descarga temporal de fotos, LongMessageBuffer (>400 chars) y ChatGuard.
"""
import os
import tempfile
from telegram import Update
from telegram.ext import ContextTypes
from core.utils import is_authorized, safe_reply
from core.executor import execution_manager
from core.message_buffer import long_message_buffer
from core.chat_guard import chat_guard
from core.obsidian import register_pending_pdf
from core.state import session_state


async def chat_guard_submit(prompt: str, message_obj, temp_files=None):
    await chat_guard.handle(prompt, message_obj, execution_manager.submit, temp_files=temp_files)


async def handle_message(update: Update, context: ContextTypes.DEFAULT_TYPE):
    if not is_authorized(update) or not update.message:
        return

    text_prompt = (update.message.text or "").strip()
    caption_prompt = (update.message.caption or "").strip()
    prompt = text_prompt or caption_prompt
    temp_files = []

    if update.message.document:
        doc = update.message.document
        fn = doc.file_name or f"documento_{doc.file_unique_id}"
        ext = os.path.splitext(fn)[1].lower()
        if (ext == ".pdf" or doc.mime_type == "application/pdf") and session_state.is_feature_enabled("obsidian"):
            try:
                file = await doc.get_file()
                safe_name = os.path.basename(fn).replace(" ", "_")
                tmp_pdf = os.path.join(tempfile.gettempdir(), f"tg_pdf_{doc.file_unique_id}_{safe_name}")
                await file.download_to_drive(tmp_pdf)
                _, markup, text = register_pending_pdf(tmp_pdf, safe_name)
                await safe_reply(update.message, text, reply_markup=markup)
            except Exception as e:
                await safe_reply(update.message, f"⚠️ Error descargando el PDF: `{e}`")
            return
        elif ext in (".pdf", ".md", ".txt", ".json", ".sql", ".ts", ".py", ".tsx"):
            try:
                file = await doc.get_file()
                tmp_doc = os.path.join(tempfile.gettempdir(), f"tg_doc_{doc.file_unique_id}_{os.path.basename(fn)}")
                await file.download_to_drive(tmp_doc)
                temp_files.append(tmp_doc)
                prompt = f"[Archivo adjunto: {tmp_doc}]\n\n{prompt or 'Revisa este archivo.'}"
            except Exception as e:
                await safe_reply(update.message, f"⚠️ Error descargando archivo: `{e}`")
                return

    if update.message.photo:
        try:
            largest_photo = update.message.photo[-1]
            file = await largest_photo.get_file()
            tmp_img = os.path.join(tempfile.gettempdir(), f"telegram_photo_{largest_photo.file_unique_id}.jpg")
            await file.download_to_drive(tmp_img)
            temp_files.append(tmp_img)

            if prompt:
                prompt = f"[Foto adjunta guardada en: {tmp_img}]\n\n{prompt}"
            else:
                prompt = f"[Foto adjunta guardada en: {tmp_img}]\nPor favor analiza esta imagen."
        except Exception as e:
            await safe_reply(update.message, f"⚠️ Error descargando la imagen: `{e}`")
            return

    if not prompt:
        return

    if session_state.is_feature_enabled("long_buffer"):
        buffered = await long_message_buffer.handle(
            update.message.chat_id, prompt, update.message, chat_guard_submit, temp_files=temp_files
        )
        if buffered:
            return

    await chat_guard_submit(prompt, update.message, temp_files=temp_files)
