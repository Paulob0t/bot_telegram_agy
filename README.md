# ⚡ Telegram Bot para Antigravity CLI (AGY) & Control Remoto de Desarrollo

Asistente remoto de **Google Antigravity (`agy`)** y centro de control para servidor de desarrollo en Linux (Wayland / Hyprland) accesible desde Telegram.


---

## ✨ Características Principales

1. **⚙️ Cola Concurrente y Gestión Limpia de Procesos (`AGYExecutionManager`):**
   - Configurado con `concurrent_updates(True)` para que comandos como `/cancel`, `/queue`, `/status` o botones inline respondan al instante incluso mientras AGY está ejecutando una tarea larga.
   - Si envías varios mensajes seguidos mientras AGY está ocupado, se encolan en orden FIFO con controles interactivos:
     - `⚡ Interrumpir y ejecutar ya` (pasa el mensaje al frente y aborta el proceso anterior).
     - `🗑️ Quitar de cola` (descarta el mensaje en espera).
     - `🛑 Cancelar todo` (aborta el proceso activo y vacía la cola).
   - Lanza cada instancia de `agy` en su propio grupo de procesos (`start_new_session=True`) y termina todo el árbol de subprocesos (`os.killpg` con `SIGTERM` → `SIGKILL`) para evitar procesos hijos huérfanos (`node`, `git`, `bash`).

2. **🗂️ Selector Dinámico de Proyecto / Workspace (`/project` o `/cd`):**
   - Lista automáticamente todas las carpetas de proyectos en `/run/media/paulobot/Datos/Projects/*` ordenadas por modificación más reciente, mostrando la rama activa de Git (`🌿 branch`).
   - Soporta búsqueda rápida por coincidencia parcial (`/cd wicode-b` activa `wicode-backend` al instante; `/cd wicode` muestra botones con las coincidencias).

3. **💾 Persistencia Atómica de Sesión (`telegram_bot_state.json`):**
   - Al reiniciar el servicio `systemd` o la laptop, restaura toda tu configuración sin perder contexto.

4. **🤖 Catálogo Dinámico de Modelos (`agy models`) y Cálculo Real de Contexto (`/usage`):**
   - Sincroniza en vivo con `agy models` desde Telegram (`/model sync` o botón inline `🔄 Sincronizar lista con agy models`), incluyendo **Gemini 4 Argon**, **Gemini 3.8 Flash**, **Claude Opus 5.5**, **Claude Sonnet 5.5** y **GPT-OSS 120B**.
   - Lee `~/.gemini/antigravity-cli/settings.json` para detectar el modelo por defecto real del CLI y calcula la barra de capacidad de tokens sobre la ventana real de cada familia (`1,048,576` tokens para Gemini, `200,000` para Claude y `128,000` para GPT-OSS).

5. **✨ Streaming Rico sin Parpadeo de Markdown + Corte Inteligente (> 4000 chars):**
   - **Sanitizador en tiempo real (`sanitize_streaming_markdown`):** Cierra temporalmente bloques de código abiertos (```` ``` ````), backticks inline (`` ` ``) o negritas (`**`) antes de cada edición en Telegram para evitar fallos de `parse_mode="Markdown"`.
   - **Inspector de herramientas en vivo (`format_step_tool_activity`):** Muestra en tiempo real qué comando está corriendo (`run_command`), qué archivo está leyendo/editando (`view_file`, `edit_file`, `create_file`) o qué está buscando AGY.
   - **División inteligente (`split_markdown_chunks`):** Cuando la respuesta final supera los 4000 caracteres, divide por párrafos (`\n\n`) o saltos de línea (`\n`), cerrando el bloque ```` ```lenguaje ```` al final de un mensaje y reabriéndolo automáticamente con el mismo lenguaje al inicio del siguiente.

   - `/screen` ejecuta `grim` sobre el display Wayland activo y envía una captura PNG del escritorio de la laptop al chat de Telegram.
     - `🔔 Siempre Activo (off)`: Reenvía todas las notificaciones del sistema al celular.
     - `🔕 No Molestar (on)`: Silencia todas las notificaciones de escritorio.

---

## 📂 Arquitectura Modular del Proyecto

```text
telegram-paulobot/
├── bot.py                          # Entrypoint principal: inicializa Application y registra handlers
├── requirements.txt                # Dependencias de Python
├── .env.example                    # Plantilla de variables de entorno (sin secretos)
├── .gitignore                      # Protección de .env, caché y logs
├── core/
│   ├── __init__.py
│   ├── config.py                   # Carga segura de .env (chmod 600), rutas y logging
│   ├── state.py                    # BotSessionState con persistencia atómica en JSON
│   ├── utils.py                    # Sanitizador Markdown, splitter de chunks y formateador de herramientas
│   ├── executor.py                 # AGYExecutionManager: cola FIFO, streaming JSON y control de grupos de procesos (PGID)
│   ├── projects.py                 # Descubrimiento de proyectos, paginación y resolución difusa para /cd
│   ├── models.py                   # Catálogo dinámico sincronizado con `agy models` y ventanas de contexto
│   ├── chats_db.py                 # Historial paginado y eliminación de conversaciones en SQLite
│   └── handlers/
│       ├── __init__.py
│       └── callbacks.py            # Enrutador modular de CallbackQuery (botones inline)
├── scripts/
│   ├── agy-hook-notify             # Hook de finalización de tareas de AGY
│   └── notify-telegram             # Utilidad CLI para enviar alertas desde terminal o pipes
└── systemd/
    ├── telegram-bot.service            # Unidad systemd de usuario para el bot principal
    └── telegram-notify-bridge.service  # Unidad systemd de usuario para el puente DBus
```

---

## 🚀 Instalación y Configuración

### 1. Requisitos del Sistema
- **Python 3.10+** y `python-telegram-bot`
- **Google Antigravity CLI (`agy`)** instalado en `~/.local/bin/agy`

### 2. Configurar Variables de Entorno (`.env`)
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

### 3. Instalar Dependencias
```bash
pip install -r requirements.txt
```

### 4. Ejecución con `systemd` (Modo Usuario)
Copia las unidades a `~/.config/systemd/user/` y habilítalas:

```bash
mkdir -p ~/.config/systemd/user
cp systemd/*.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now telegram-bot.service
systemctl --user enable --now telegram-notify-bridge.service
```

---

## 📋 Referencia Rápida de Comandos en Telegram

| Comando | Alias | Descripción |
| :--- | :--- | :--- |
| `/start` | `/help`, `/ayuda` | Muestra el panel general de ayuda y el estado actual |
| `/project` | `/cd`, `/workspace`, `/repo` | Abre el selector de proyectos o cambia rápido (`/cd wicode-b`) |
| `/screen` | `/ss`, `/captura` | Toma una captura de pantalla del escritorio Wayland (`grim`) y la envía al chat |
| `/queue` | `/cola` | Muestra la tarea en ejecución y la cola de mensajes en espera |
| `/cancel` | `/stop`, `/cancelar` | Cancela el árbol de procesos de la tarea activa de AGY |
| `/model` | `/modelo` | Cambia el modelo de IA o sincroniza el catálogo con `agy models` (`/model sync`) |
| `/effort` | `/esfuerzo`, `/speed` | Ajusta el nivel de razonamiento (`low`, `medium`, `high`, `xhigh`, `max`) |
| `/usage` | `/tokens`, `/consumo` | Muestra barra de capacidad libre y consumo de tokens del último turno |
| `/resume` | `/chats`, `/historial` | Historial paginado de conversaciones de AGY guardadas en SQLite |
| `/delete` | `/borrar`, `/eliminar` | Menú para eliminar conversaciones antiguas de SQLite y del disco |
| `/continue` | `/continuar` | Configura el envío en modo `-c` (continuar última conversación activa) |
| `/new` | `/nuevo` | Configura el siguiente mensaje para abrir un hilo de conversación nuevo |
| `/current` | `/actual` | Muestra el resumen de configuración persistente activa |
| `/status` | `/estado` | Muestra estado de AGY, cola, RAM y disco |
| `/off` | `/power`, `/shutdown` | Apaga de forma segura el servicio `telegram-bot.service` |
