# ⚡ Telegram Bot para Antigravity CLI (AGY) & Quickshell Ecosystem

Asistente remoto de **Google Antigravity (`agy`)**, centro de control para desarrollo en Linux (Wayland / Hyprland / Omarchy) y ecosistema unificado de widgets **Quickshell** y hooks de notificación de **0 tokens**.

---

## ✨ Características Principales

1. **⚙️ Cola Concurrente y Gestión Limpia de Procesos (`AGYExecutionManager`):**
   - Configurado con `concurrent_updates(True)` para que comandos como `/cancel`, `/queue`, `/status` o botones inline respondan al instante incluso mientras AGY está ejecutando una tarea larga.
   - Si envías varios mensajes seguidos mientras AGY está ocupado, se encolan en orden FIFO con controles interactivos:
     - `⚡ Interrumpir y ejecutar ya` (pasa el mensaje al frente y aborta el proceso anterior).
     - `🗑️ Quitar de cola` (descarta el mensaje en espera).
     - `🛑 Cancelar todo` (aborta el proceso activo y vacía la cola).
   - Lanza cada instancia de `agy` en su propio grupo de procesos (`start_new_session=True`) y termina todo el árbol de subprocesos (`os.killpg` con `SIGTERM` → `SIGKILL`) para evitar procesos hijos huérfanos (`node`, `git`, `bash`).

2. **🧩 Buffer de Mensajes Largos (`LongMessageBuffer`) y Guardia de Sesión (`ChatGuard`):**
   - **Buffer automático (`> 400` caracteres):** Cuando pegas logs o bloques largos que Telegram divide en varios mensajes, el bot los agrupa en un buffer interactivo con botones (`🚀 Enviar todo`, `➕ Seguir pegando`, `🗑️ Descartar`) para enviarlos en un solo prompt.
   - **Guardia de inactividad (`> 30 min`):** Si vuelves a escribir tras 30 minutos de inactividad, el bot retiene el mensaje y te permite elegir en qué conversación ejecutarlo (`💬 Seguir actual`, `🆕 Nuevo chat` o cualquiera de los últimos chats del proyecto).

3. **📸 Soporte Nativo de Imágenes y Capturas Wayland (`/screen`):**
   - Envía fotos o capturas comprimidas/sin comprimir directamente al chat de Telegram; el bot las descarga en `/tmp/telegram-paulobot-media` e inyecta su ruta junto con tu instrucción para que AGY las inspeccione.
   - `/screen` ejecuta `grim` sobre el display Wayland activo y envía una captura PNG del escritorio al chat.

4. **📊 Panel de Cuotas Unificado (`/usage` & Quickshell `paulobot.ai-tokens`):**
   - Comparte el mismo backend (`scripts/ai-token-stats`) entre el comando `/usage` de Telegram y el widget de la barra Quickshell.
   - Consulta **bajo demanda (on-demand)** las cuotas de **Claude Code** (`claude --no-session-persistence -p /usage`, sin generar archivos `.jsonl` basura) y **Antigravity** (`agy -p /usage`: Gemini 5h/Semanal, Claude & GPT 5h/Semanal y Gemini 4 Argon Semanal).

5. **🔔 Hooks de Notificación `Stop` con 0 Consumo de Tokens (`agy-hook-notify` & `claude-hook-notify`):**
   - Al terminar cualquier tarea en terminal con **Antigravity (`agy`)** o **Claude Code (`claude`)**, los hooks leen directamente el transcript `.jsonl` local sin invocar al LLM (0 tokens):
     - **Omarchy Desktop:** Muestra únicamente un aviso minimalista `✓ Tarea lista · <Proyecto>`.
     - **Telegram:** Envía el desglose completo de la respuesta final del modelo directo a tu celular.

6. **🗂️ Selector Dinámico de Proyecto (`/project` o `/cd`) y Catálogo de Modelos (`/model`):**
   - Lista automáticamente los repositorios en `/run/media/paulobot/Datos/Projects/*` ordenados por modificación reciente y muestra su rama activa de Git (`🌿 branch`).
   - Sincroniza en vivo con `agy models` (`/model sync`) y persiste toda la sesión de forma atómica en `~/.gemini/antigravity-cli/telegram_bot_state.json`.

---

## 📂 Arquitectura Modular del Proyecto

```text
telegram-paulobot/
├── bot.py                          # Entrypoint principal: inicializa Application y registra handlers
├── requirements.txt                # Dependencias de Python
├── .env.example                    # Plantilla de variables de entorno (sin secretos)
├── core/
│   ├── __init__.py
│   ├── config.py                   # Carga segura de .env (chmod 600), rutas y constantes
│   ├── state.py                    # BotSessionState con persistencia atómica en JSON
│   ├── utils.py                    # Sanitizador Markdown, splitter de chunks, modo ultracave y formateador de tools
│   ├── buffer.py                   # LongMessageBuffer (>400 chars) y ChatGuard (selector tras 30 min inactivo)
│   ├── executor.py                 # AGYExecutionManager: cola FIFO, streaming JSON y control de grupos de procesos (PGID)
│   ├── projects.py                 # Descubrimiento de proyectos, paginación y resolución difusa para /cd
│   ├── models.py                   # Catálogo dinámico (`agy models`) y dashboard de cuotas (`ai-token-stats`)
│   ├── chats_db.py                 # Historial paginado y eliminación de conversaciones en SQLite
│   └── handlers/
│       ├── __init__.py
│       ├── commands.py             # Comandos (/start, /project, /model, /usage, /resume, /screen, etc.)
│       ├── callbacks.py            # Enrutador modular de CallbackQuery (botones inline)
│       └── messages.py             # Handler de texto, fotos, documentos de imagen, buffer y ChatGuard
├── quickshell/
│   ├── paulobot.ai-tokens/         # Widget Quickshell de cuotas AI (consulta on-demand al abrir o refrescar)
│   └── paulobot.telegram-bot/      # Widget Quickshell de estado y encendido/apagado de telegram-bot.service
├── scripts/
│   ├── ai-token-stats              # Backend unificado de cuotas (Claude Code + Antigravity, 0 sesiones basura)
│   ├── agy-hook-notify             # Hook Stop de AGY (0 tokens: check en Omarchy + desglose en Telegram)
│   ├── claude-hook-notify          # Hook Stop de Claude Code (0 tokens: check en Omarchy + desglose en Telegram)
│   ├── notify-telegram             # Utilidad CLI para enviar alertas manuales desde terminal o pipes
│   └── telegram-bot-service        # Wrapper CLI de systemctl --user para controlar telegram-bot.service
└── systemd/
    └── telegram-bot.service        # Unidad systemd de usuario para el bot principal
```

---

## 🚀 Instalación y Configuración

### 1. Variables de Entorno (`.env`)
Copia la plantilla `.env.example` a `.env` y restringe sus permisos a `600`:

```bash
cp .env.example .env
chmod 600 .env
```

Edita `.env` con tus credenciales:
```env
TELEGRAM_BOT_TOKEN=tu_token_de_botfather_aqui
TELEGRAM_AUTHORIZED_USER_ID=tu_user_id_numerico_aqui
TELEGRAM_CHAT_ID=tu_chat_id_numerico_aqui
```

### 2. Symlinks Unificados (Scripts y Quickshell)
Todos los scripts y widgets viven dentro de este repositorio y se enlazan a `~/.local/bin` y `~/.config/omarchy/plugins`:

```bash
ln -sf "$PWD/scripts/ai-token-stats" ~/.local/bin/ai-token-stats
ln -sf "$PWD/scripts/agy-hook-notify" ~/.local/bin/agy-hook-notify
ln -sf "$PWD/scripts/claude-hook-notify" ~/.local/bin/claude-hook-notify
ln -sf "$PWD/scripts/telegram-bot-service" ~/.local/bin/telegram-bot-service

ln -sfn "$PWD/quickshell/paulobot.ai-tokens" ~/.config/omarchy/plugins/paulobot.ai-tokens
ln -sfn "$PWD/quickshell/paulobot.telegram-bot" ~/.config/omarchy/plugins/paulobot.telegram-bot
```

### 3. Servicio `systemd`
```bash
mkdir -p ~/.config/systemd/user
cp systemd/telegram-bot.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now telegram-bot.service
```

---

## 📋 Referencia Rápida de Comandos en Telegram

| Comando | Alias | Descripción |
| :--- | :--- | :--- |
| `/start` | `/help`, `/ayuda` | Muestra el panel general de ayuda y el estado actual |
| `/project` | `/cd`, `/workspace`, `/repo` | Abre el selector de proyectos o cambia rápido (`/cd stover`) |
| `/screen` | `/ss`, `/captura` | Toma una captura de pantalla del escritorio Wayland (`grim`) y la envía al chat |
| `/queue` | `/cola` | Muestra la tarea en ejecución y la cola de mensajes en espera |
| `/cancel` | `/stop`, `/cancelar` | Cancela el árbol de procesos de la tarea activa de AGY |
| `/model` | `/modelo` | Cambia el modelo de IA o sincroniza el catálogo con `agy models` (`/model sync`) |
| `/effort` | `/esfuerzo`, `/speed` | Ajusta el nivel de razonamiento (`low`, `medium`, `high`, `xhigh`, `max`) |
| `/usage` | `/tokens`, `/consumo` | Muestra el panel de cuotas en vivo de Claude Code y Antigravity (Quickshell Sync) |
| `/resume` | `/chats`, `/historial` | Historial paginado de conversaciones de AGY guardadas en SQLite |
| `/delete` | `/borrar`, `/eliminar` | Menú para eliminar conversaciones antiguas de SQLite y del disco |
| `/continue` | `/continuar` | Configura el envío en modo `-c` (continuar última conversación activa) |
| `/new` | `/nuevo` | Configura el siguiente mensaje para abrir un hilo de conversación nuevo |
| `/current` | `/actual` | Muestra el resumen de configuración persistente activa |
| `/status` | `/estado` | Muestra estado de AGY, cola, RAM y disco |
| `/off` | `/power`, `/shutdown` | Apaga de forma segura el servicio `telegram-bot.service` |
