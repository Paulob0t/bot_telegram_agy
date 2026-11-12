import os
import json
import time
import uuid
import signal
import asyncio
import traceback
from dataclasses import dataclass, field
from typing import Optional, List, Any
from telegram import InlineKeyboardButton, InlineKeyboardMarkup
from core.config import AGY_BIN, CLAUDE_BIN, HOME_DIR, IS_WINDOWS, logger, trim_process_memory
from core.state import session_state
from core.utils import (
    safe_reply,
    safe_edit,
    format_uptime,
    sanitize_streaming_markdown,
    format_step_tool_activity,
    split_markdown_chunks,
)
from core.chats_db import get_all_conversations
from core.claude_chats import is_claude_busy_in_cwd


def _get_latest_conversation():
    """(conversation_id, title) del chat de AGY modificado más recientemente, o None."""
    rows = get_all_conversations()
    if not rows:
        return None
    conv_id, title, _ = rows[0]
    return conv_id, (title or "").strip().replace("\n", " ")


@dataclass
class QueuedJob:
    job_id: str
    prompt: str
    message_obj: Any
    enqueued_at: float = field(default_factory=time.time)
    queue_notice_msg: Any = None
    engine: str = "agy"
    conv_id: Optional[str] = None
    conv_title: str = ""
    model: Optional[str] = None
    effort: str = "low"
    cwd: str = HOME_DIR
    temp_files: List[str] = field(default_factory=list)


class AGYExecutionManager:
    """
    Garantiza que solo exista un proceso de AGY corriendo a la vez por sesión,
    encola mensajes entrantes sin sobrescribir procesos activos, ejecuta cada
    trabajo en el directorio de proyecto (`cwd`) configurado y mata grupos de
    procesos completos (PGID) para evitar procesos hijos huérfanos.
    """

    def __init__(self):
        self._lock = asyncio.Lock()
        self.active_process: Optional[asyncio.subprocess.Process] = None
        self.active_job: Optional[QueuedJob] = None
        self.active_started_at: Optional[float] = None
        self.was_cancelled: bool = False
        self.queue: List[QueuedJob] = []
        self._worker_task: Optional[asyncio.Task] = None

    def is_busy(self) -> bool:
        if self.active_job is not None:
            return True
        if self.active_process is not None and self.active_process.returncode is None:
            return True
        return False

    def get_active_elapsed(self) -> int:
        if not self.active_started_at:
            return 0
        return max(0, int(time.time() - self.active_started_at))

    async def cancel_active(self) -> bool:
        """Mata el árbol de procesos del trabajo activo sin dejar procesos huérfanos."""
        proc = self.active_process
        if not proc or proc.returncode is not None:
            return False

        self.was_cancelled = True
        pid = proc.pid
        if IS_WINDOWS:
            try:
                kill_proc = await asyncio.create_subprocess_exec(
                    "taskkill", "/F", "/T", "/PID", str(pid),
                    stdout=asyncio.subprocess.DEVNULL,
                    stderr=asyncio.subprocess.DEVNULL,
                )
                await asyncio.wait_for(kill_proc.wait(), timeout=3.0)
            except Exception:
                try:
                    proc.kill()
                except Exception:
                    pass
        else:
            try:
                pgid = os.getpgid(pid)
                os.killpg(pgid, signal.SIGTERM)
            except Exception:
                try:
                    proc.terminate()
                except Exception:
                    pass

            try:
                await asyncio.wait_for(proc.wait(), timeout=1.5)
            except asyncio.TimeoutError:
                try:
                    pgid = os.getpgid(pid)
                    os.killpg(pgid, signal.SIGKILL)
                except Exception:
                    try:
                        proc.kill()
                    except Exception:
                        pass
                try:
                    await asyncio.wait_for(proc.wait(), timeout=1.0)
                except Exception:
                    pass

        logger.info(f"Proceso AGY (PID {pid}) y subprocesos terminados limpiamente.")
        return True

    async def drop_queued_job(self, job_id: str) -> Optional[QueuedJob]:
        async with self._lock:
            for idx, item in enumerate(self.queue):
                if item.job_id == job_id:
                    removed = self.queue.pop(idx)
                    for tf in removed.temp_files:
                        if tf and os.path.exists(tf):
                            try:
                                os.remove(tf)
                            except Exception:
                                pass
                    return removed
            return None

    async def clear_queue(self) -> int:
        async with self._lock:
            items = list(self.queue)
            self.queue.clear()
        for item in items:
            for tf in item.temp_files:
                if tf and os.path.exists(tf):
                    try:
                        os.remove(tf)
                    except Exception:
                        pass
            if item.queue_notice_msg:
                await safe_edit(item.queue_notice_msg, "🗑️ *Mensaje descartado (cola vaciada).*")
        return len(items)

    async def force_job_now(self, job_id: str) -> bool:
        """Mueve un trabajo encolado al frente e interrumpe inmediatamente el proceso activo."""
        async with self._lock:
            target_idx = None
            for idx, item in enumerate(self.queue):
                if item.job_id == job_id:
                    target_idx = idx
                    break
            if target_idx is None:
                return False

            job = self.queue.pop(target_idx)
            self.queue.insert(0, job)
            job.queue_notice_msg = None

        await self.cancel_active()
        return True

    async def submit(
        self,
        prompt: str,
        message_obj: Any,
        model_override: Optional[str] = None,
        effort_override: Optional[str] = None,
        temp_files: Optional[List[str]] = None,
    ):
        is_claude = session_state.current_engine == "claude"
        chosen_model = (
            (model_override if model_override else session_state.claude_model)
            if is_claude
            else (model_override if model_override else session_state.current_model)
        )
        chosen_effort = effort_override if effort_override else session_state.current_effort
        if chosen_model and not is_claude:
            for eff in ("low", "medium", "high"):
                if chosen_model.lower().endswith(f"-{eff}"):
                    chosen_effort = eff
                    break

        active_conv_id = session_state.claude_session_id if is_claude else session_state.current_conversation_id
        active_conv_title = session_state.claude_session_title if is_claude else session_state.current_conversation_title

        job = QueuedJob(
            job_id=uuid.uuid4().hex[:8],
            prompt=prompt,
            message_obj=message_obj,
            engine="claude" if is_claude else "agy",
            conv_id=active_conv_id,
            conv_title=active_conv_title,
            model=chosen_model,
            effort=chosen_effort,
            cwd=session_state.current_cwd,
            temp_files=list(temp_files) if temp_files else [],
        )

        # Si estaba configurado en NEW, el siguiente mensaje después de este continuará ese hilo
        if is_claude and session_state.claude_session_id == "NEW":
            session_state.claude_session_id = None
            session_state.claude_session_title = "🔄 Último chat de Claude (-c)"
            session_state.save_state()
        elif not is_claude and session_state.current_conversation_id == "NEW":
            session_state.current_conversation_id = None
            session_state.current_conversation_title = "🔄 Continuar último chat activo (-c)"
            session_state.save_state()

        async with self._lock:
            already_busy = self.is_busy() or len(self.queue) > 0
            self.queue.append(job)
            position = len(self.queue)

            if self._worker_task is None or self._worker_task.done():
                self._worker_task = asyncio.create_task(self._worker_loop())

        if already_busy:
            elapsed_str = format_uptime(self.get_active_elapsed())
            preview = prompt.replace("\n", " ")
            if len(preview) > 90:
                preview = preview[:90] + "..."
            cwd_short = session_state.get_cwd_short_name(job.cwd)

            keyboard = [
                [
                    InlineKeyboardButton("⚡ Interrumpir y ejecutar ya", callback_data=f"q_force:{job.job_id}"),
                    InlineKeyboardButton("🗑️ Quitar de cola", callback_data=f"q_drop:{job.job_id}"),
                ],
                [
                    InlineKeyboardButton("🛑 Cancelar todo", callback_data="q_cancel_all")
                ],
            ]
            notice = await safe_reply(
                message_obj,
                f"⏳ **AGY está ocupado** *(tarea actual: `{elapsed_str}`)*.\n"
                f"📥 **Tu mensaje quedó en cola (Posición `#{position}` • 📂 `{cwd_short}`):**\n"
                f"`{preview}`\n\n"
                f"*Se ejecutará automáticamente al terminar la tarea actual, o elige una opción:*",
                reply_markup=InlineKeyboardMarkup(keyboard),
            )
            job.queue_notice_msg = notice

    def _pin_conversation_after_run(self, job: "QueuedJob") -> Optional[str]:
        """
        Si el job corrió con -c o como chat nuevo, toma el chat más reciente de la
        base de AGY y lo fija como chat activo. Solo lo hace si el usuario no
        eligió otro chat mientras corría. Retorna el título fijado o None.
        """
        if job.conv_id not in (None, "NEW", "LATEST"):
            return None
        if session_state.current_conversation_id is not None:
            return None
        latest = _get_latest_conversation()
        if not latest:
            return None
        conv_id, title = latest[0], latest[1]
        session_state.set_conversation(conv_id, f"💬 {title}")
        logger.info("Chat fijado tras ejecución: %s (%s)", title, conv_id)
        return title

    async def _worker_loop(self):
        while True:
            async with self._lock:
                if not self.queue:
                    self.active_job = None
                    return
                job = self.queue.pop(0)
                self.active_job = job
                self.active_started_at = time.time()
                self.was_cancelled = False

            try:
                await self._execute_single_job(job)
            except Exception:
                logger.error(f"Error inesperado en worker loop de AGY: {traceback.format_exc()}")
            finally:
                self.active_process = None
                self.active_job = None
                self.active_started_at = None

    async def _execute_single_job(self, job: QueuedJob):
        prompt = f"{job.prompt}\n\n[Telegram: responde ultracave, máx 4 líneas cortas salvo que pida detalle]"
        is_claude = job.engine == "claude"

        if is_claude:
            command = [
                CLAUDE_BIN,
                "-p", prompt,
                "--output-format", "stream-json",
                "--verbose",
                "--dangerously-skip-permissions",
            ]
            if job.model:
                command.extend(["--model", job.model])
            if job.effort:
                eff_cl = job.effort if job.effort in ("low", "medium", "high") else "high"
                command.extend(["--effort", eff_cl])

            exec_cwd_check = job.cwd if (job.cwd and os.path.isdir(job.cwd)) else HOME_DIR
            term_busy = is_claude_busy_in_cwd(
                exec_cwd_check,
                job.conv_id if (job.conv_id and job.conv_id not in ("LATEST", "NEW")) else None,
            )

            if job.conv_id and job.conv_id not in ("LATEST", "NEW"):
                command.extend(["-r", job.conv_id])
                if term_busy:
                    command.append("--fork-session")
                    chat_label = f"🟠 {job.conv_title} (Fork Anti-Colisión)"
                else:
                    chat_label = f"🟠 {job.conv_title}"
            elif job.conv_id == "NEW":
                chat_label = "🟠 Nuevo Chat Claude"
            else:
                command.append("-c")
                if term_busy:
                    command.append("--fork-session")
                    chat_label = f"🟠 {job.conv_title or 'Último Chat Claude'} (Fork Anti-Colisión)"
                else:
                    chat_label = f"🟠 {job.conv_title or 'Último Chat Claude'}"
        else:
            command = [
                AGY_BIN,
                "-p", prompt,
                "--output-format", "stream-json",
                "--dangerously-skip-permissions",
            ]

            if job.model:
                command.extend(["--model", job.model])
                if not job.model.lower().endswith(("-low", "-medium", "-high")) and job.effort:
                    command.extend(["--effort", job.effort])
            elif job.effort:
                command.extend(["--effort", job.effort])

            if job.conv_id and job.conv_id not in ("LATEST", "NEW"):
                command.extend(["--conversation", job.conv_id])
                chat_label = job.conv_title
            elif job.conv_id == "NEW":
                chat_label = "Nuevo Chat"
            else:
                command.append("-c")
                # Mostrar el título real del último chat en vez de un genérico
                latest = _get_latest_conversation()
                chat_label = f"💬 {latest[1]}" if latest else "Último Chat Activo"

        exec_cwd = job.cwd if (job.cwd and os.path.isdir(job.cwd)) else HOME_DIR
        cwd_short = session_state.get_cwd_short_name(exec_cwd)

        cancel_markup = InlineKeyboardMarkup([
            [InlineKeyboardButton("🛑 Cancelar ejecución", callback_data="q_cancel_active")]
        ])

        header_text = f"⚡ **[{chat_label}]** • 📂 `{cwd_short}`\n⏳ *Pensando...*"

        if job.queue_notice_msg:
            status_msg = job.queue_notice_msg
            await safe_edit(status_msg, header_text, reply_markup=cancel_markup)
        else:
            status_msg = await safe_reply(job.message_obj, header_text, reply_markup=cancel_markup)

        env = os.environ.copy()
        if IS_WINDOWS:
            extra_dirs = [
                os.path.expanduser(r"~\AppData\Local\agy\bin"),
                os.path.expanduser(r"~\AppData\Roaming\npm"),
                os.path.expanduser(r"~\.local\bin"),
            ]
        else:
            extra_dirs = [
                os.path.expanduser("~/.local/bin"),
                os.path.expanduser("~/.gemini/antigravity-cli/bin"),
            ]
        env["PATH"] = os.pathsep.join(extra_dirs + [env.get("PATH", "")])
        env["AGY_FROM_TELEGRAM"] = "1"
        env["CLAUDE_FROM_TELEGRAM"] = "1"

        try:
            proc_kwargs = {"limit": 2 * 1024 * 1024}
            if IS_WINDOWS:
                import subprocess as _sp
                proc_kwargs["creationflags"] = getattr(_sp, "CREATE_NO_WINDOW", 0x08000000)
            else:
                proc_kwargs["start_new_session"] = True
            proc = await asyncio.create_subprocess_exec(
                *command,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.STDOUT,
                cwd=exec_cwd,
                env=env,
                **proc_kwargs,
            )
            self.active_process = proc

            accumulated_text = ""
            current_tool_action = ""
            is_stream_finished = False
            last_rendered_content = ""

            async def read_stdout_loop():
                nonlocal accumulated_text, current_tool_action, is_stream_finished
                try:
                    while True:
                        line_bytes = await proc.stdout.readline()
                        if not line_bytes:
                            break

                        line_str = line_bytes.decode("utf-8", errors="replace").strip()
                        if not line_str:
                            continue

                        try:
                            event_data = json.loads(line_str)
                            event_type = event_data.get("event") or event_data.get("type")

                            if event_type == "step_update":
                                step = event_data.get("step_update", {})
                                stype = step.get("step_type")

                                if "usage" in step and step["usage"]:
                                    session_state.save_usage_cache(step["usage"])

                                if stype == "agent_response" or "text_delta" in step:
                                    delta = step.get("text_delta", "")
                                    if delta:
                                        accumulated_text += delta
                                        current_tool_action = ""

                                tool_activity = format_step_tool_activity(step)
                                if tool_activity:
                                    current_tool_action = tool_activity
                                elif (step.get("thinking_delta") or step.get("thinking")) and not accumulated_text:
                                    current_tool_action = "\n\n🧠 *Razonando...*"

                            elif event_type == "assistant":
                                msg_obj = event_data.get("message", {})
                                content_list = msg_obj.get("content", [])
                                if isinstance(content_list, list):
                                    text_parts = []
                                    for blk in content_list:
                                        if not isinstance(blk, dict):
                                            continue
                                        btype = blk.get("type")
                                        if btype == "text" and blk.get("text"):
                                            text_parts.append(blk["text"])
                                        elif btype == "tool_use":
                                            tname = blk.get("name", "tool")
                                            current_tool_action = f"\n\n🛠️ *Ejecutando `{tname}`...*"
                                    if text_parts:
                                        accumulated_text = "\n".join(text_parts)
                                        current_tool_action = ""

                            elif event_type == "result":
                                res = event_data.get("result")
                                if isinstance(res, dict):
                                    if "usage" in res and res["usage"]:
                                        session_state.save_usage_cache(res["usage"])
                                    final_resp = res.get("response")
                                    if final_resp:
                                        accumulated_text = final_resp
                                elif isinstance(res, str) and res.strip():
                                    accumulated_text = res.strip()
                                if event_data.get("session_id") and is_claude and session_state.claude_session_id is None:
                                    session_state.claude_session_id = event_data["session_id"]
                                    session_state.save_state()
                                current_tool_action = ""

                        except Exception:
                            accumulated_text += line_str + "\n"
                finally:
                    is_stream_finished = True

            async def telegram_updater_loop():
                nonlocal last_rendered_content
                while not is_stream_finished and not self.was_cancelled:
                    await asyncio.sleep(1.2)
                    raw_state = (accumulated_text + current_tool_action).strip()
                    if raw_state and raw_state != last_rendered_content:
                        # Sanitizar y cerrar bloques ``` o ` abiertos en el texto principal antes de anexar el estado de herramienta
                        safe_body = sanitize_streaming_markdown(accumulated_text.strip(), max_len=3650)
                        rendered = f"{safe_body}{current_tool_action}\n\n⏳ ▍".strip()
                        if status_msg:
                            await safe_edit(status_msg, rendered, reply_markup=cancel_markup)
                        last_rendered_content = raw_state

            reader_task = asyncio.create_task(read_stdout_loop())
            updater_task = asyncio.create_task(telegram_updater_loop())

            await reader_task
            await updater_task
            await proc.wait()
            self.active_process = None

            final_output = accumulated_text.strip()

            if self.was_cancelled:
                if final_output:
                    safe_partial = sanitize_streaming_markdown(final_output, max_len=3750)
                    cancelled_text = f"{safe_partial}\n\n🛑 **Ejecución interrumpida / cancelada.**"
                else:
                    cancelled_text = "🛑 **Ejecución cancelada por el usuario.**"
                if status_msg:
                    await safe_edit(status_msg, cancelled_text, reply_markup=None)
                return

            if not final_output:
                final_output = "✅ Tarea completada."

            chunks = split_markdown_chunks(final_output, max_chars=3900)

            # Fijar el chat concreto usado: así el siguiente mensaje no depende de
            # "-c" (que podría apuntar a otra sesión abierta desde la laptop).
            pinned_title = None if is_claude else self._pin_conversation_after_run(job)
            footer_title = pinned_title or job.conv_title or chat_label
            engine_tag = "🟠 Claude" if is_claude else "💬"
            footer = f"\n\n— {engine_tag} _{footer_title.lstrip('🟠💬➕🔄 ').strip()[:60]}_ • 📂 `{cwd_short}`"
            if len(chunks[-1]) + len(footer) <= 4000:
                chunks[-1] = chunks[-1] + footer
            else:
                chunks.append(footer.strip())

            first_chunk = chunks[0]

            if status_msg:
                await safe_edit(status_msg, first_chunk, reply_markup=None)
            else:
                await safe_reply(job.message_obj, first_chunk)

            for extra_chunk in chunks[1:]:
                await safe_reply(job.message_obj, extra_chunk)

        except Exception as e:
            self.active_process = None
            err_msg = f"❌ Error ejecutando AGY: {e}"
            logger.error(f"Error en _execute_single_job: {traceback.format_exc()}")
            if status_msg:
                await safe_edit(status_msg, err_msg, reply_markup=None)
            else:
                await safe_reply(job.message_obj, err_msg)
        finally:
            for tf in job.temp_files:
                if tf and os.path.exists(tf):
                    try:
                        os.remove(tf)
                    except Exception:
                        pass
            trim_process_memory()


execution_manager = AGYExecutionManager()
