# Paulobot — Bot de Telegram para Antigravity (AGY) y Claude Code

Bot de control remoto y asistente de desarrollo en **Go puro** (sin dependencias externas), diseñado para operar **Antigravity CLI (`agy`)** y **Claude Code (`claude`)** desde Telegram con consumo mínimo de recursos (**~10 MB RAM**).

---

## Características principales

- **Binario único sin dependencias**: compilado en Go nativo (`go build -o telegram-bot ./cmd/bot`), sin entornos virtuales ni paquetes externos.
- **Doble motor en streaming**: alterna al vuelo entre `agy` y `claude` con cancelación inmediata (`/stop`).
- **Gestión de proyectos y sesiones**: selector interactivo de repositorios (`/project`), ramas Git detectadas sin subprocesos y reanudación de sesiones (`/resume`).
- **Cuotas de IA unificadas (`/tokens`)**: consulta en vivo y caché automática cada 60s en `~/.gemini/antigravity-cli/waybar_tokens.json` para Waybar dentro del mismo proceso.
- **Integración con escritorio Linux (Hyprland / Wayland)**: capturas de pantalla (`/screen` vía `grim`), lectura de notas de Obsidian (`/obsidian`, `/prompts`) y notificaciones en Dunst.
- **Buffer de mensajes largos & Guard de inactividad**: agrupa fragmentos pegados por partes y protege la sesión ante inactividad prolongada.

---

## Arquitectura del proyecto

```text
telegram-paulobot/
├── cmd/
│   └── bot/
│       └── main.go              # Entrypoint, polling, comandos y loop Waybar
├── internal/
│   ├── buffer/                  # Agrupador de mensajes largos
│   ├── chats/                   # Descubrimiento O(1) de sesiones Claude (.jsonl)
│   ├── config/                  # Carga de .env y resolución de binarios
│   ├── desktop/                 # Capturas (grim) y avisos Dunst (notify-send)
│   ├── executor/                # Ejecución en streaming de agy / claude
│   ├── guard/                   # Control de inactividad de sesión
│   ├── models/                  # Catálogo de modelos y ventanas de contexto
│   ├── obsidian/                # Lectura de notas y prompts del vault
│   ├── projects/                # Listado de proyectos y detección Git
│   ├── state/                   # Estado persistente atómico con sync.RWMutex
│   ├── telegram/                # Cliente nativo Telegram Bot API (net/http)
│   ├── tokens/                  # Cuotas de IA y caché JSON para Waybar
│   └── utils/                   # Formateo de uptime, Git y Markdown streaming
├── scripts/                     # Hooks de notificación (agy-hook-notify, claude-hook-notify)
├── go.mod
└── README.md
```

---

## Comandos de Telegram

| Comando | Descripción |
| :--- | :--- |
| `/start`, `/status` | Estado del bot, motor activo, proyecto actual y uptime |
| `/engine` | Cambiar motor entre **AGY** y **Claude** |
| `/project`, `/cd` | Seleccionar directorio de trabajo |
| `/model` | Cambiar modelo activo |
| `/resume`, `/chats` | Reanudar una sesión reciente de Claude o iniciar chat nuevo |
| `/tokens` | Consultar cuotas actuales de Claude y Antigravity |
| `/obsidian`, `/prompts` | Listar y leer notas `.md` del vault |
| `/screen` | Capturar pantalla actual y enviarla por Telegram |
| `/stop` | Cancelar la ejecución en curso |

---

## Compilación y despliegue

```bash
# Ejecutar tests unitarios
go test ./...

# Compilar binario único
go build -o telegram-bot ./cmd/bot

# Reiniciar servicio systemd de usuario
systemctl --user restart telegram-bot.service
```

### Variables de entorno (`.env`)

```env
TELEGRAM_BOT_TOKEN=tu_token
TELEGRAM_AUTHORIZED_USER_ID=tu_user_id
BOT_PROJECTS_DIR=/mnt/datos/Projects
BOT_OBSIDIAN_DIR=/mnt/datos/Documents/wicode
```
