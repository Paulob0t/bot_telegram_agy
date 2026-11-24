# HyprAgent (`agente-bot`) — Core Go + App Móvil para Antigravity (AGY) y Claude Code

Bot y servidor WebSocket de control remoto, gestión de sesiones, recepción de archivos y monitor de cuotas de IA escrito **100% en Go puro** (`0 dependencias externas`, `0 Python`) con cliente móvil Flutter (`mobile_app/`), diseñado para operar **Antigravity CLI (`agy`)** y **Claude Code (`claude`)** con consumo mínimo de recursos (**~1.5 MB – 10 MB RAM**).

---

## Características principales

- **Binario único 100% Go (0 Python)**: compilado nativo (`go build -o telegram-bot ./cmd/bot`) que integra en un solo ejecutable el bot de Telegram, el actualizador de Waybar, el diálogo interactivo `rofi` y los hooks CLI de notificaciones.
- **Doble motor en streaming (`stream-json`)**: alterna al vuelo entre `agy` y `claude` mostrando herramientas, comandos ejecutados y archivos editados en tiempo real.
- **Cancelación instantánea de árbol de procesos (`/proc`)**: recorre recursivamente `/proc/<pid>/task/*/children`, mata subprocesos nietos (`npm`, `pip`, `bun`, etc.) aunque creen su propio `setsid`/`pgid` y cierra `stdout` al instante (`<50ms`), con opción de **⚡ Interrumpir y ejecutar ya** desde la cola.
- **ChatGuard con selector de Proyecto (`cwd`)**: tras reinicio o inactividad retiene el mensaje y permite confirmar el chat actual, crear uno nuevo, elegir otro chat paginado o **cambiar de proyecto (`cwd`)** antes de enviar, recordando el `cwd` asociado a cada conversación (`conversation_cwd_map`).
- **Recepción de PDFs, fotos y archivos (Telegram → PC)**:
  - Caché persistente por 24h en `~/.cache/telegram-paulobot/uploads/` para hacer múltiples preguntas sobre la misma imagen o documento.
  - Menú interactivo al enviar archivos/PDFs/fotos: `🤖 Analizar con IA`, `📂 Guardar en proyecto (cwd)`, `📁 Otro proyecto…`, `📥 Guardar en ~/Downloads`, y para PDFs `📒 Guardar en Obsidian (<Proyecto>/PDF · 0 tokens)` o `⚡ Guardar + Generar Nota y Prompts Wicode (T01..TXX)`.
- **Cuotas de IA con fecha exacta y cuenta regresiva**: consulta concurrente de Claude Code y Antigravity mostrando hora exacta de reinicio y tiempo restante (`↻ Hoy 02:00 PM (en 16m)`, `↻ Mar 24 Nov, 05:28 PM (en 6d 3h)`) en `/tokens`, tooltip de Waybar y panel `rofi`.

---

## Arquitectura modular

```text
agente-bot/
├── cmd/
│   └── bot/
│       └── main.go              # Entrypoint ligero (63 líneas) y enrutador de subcomandos CLI
├── mobile_app/                  # App móvil Flutter (WebSocket + OTA + Avatar Rive + Voz)
├── internal/
│   ├── bot/                     # Núcleo modular del bot (sin variables globales)
│   │   ├── bot.go               # Struct App, loop de polling y recepción de fotos/archivos
│   │   ├── queue.go             # Cola serializada, streaming, interrupción y cancelación
│   │   ├── commands.go          # Manejador de comandos (/start, /model, /project, /resume, etc.)
│   │   ├── callbacks.go         # Enrutador de botones inline por dominio (cola, uploads, guard, sistema)
│   │   └── views.go             # Constructores de vistas y teclados inline paginados
│   ├── buffer/                  # Agrupador de mensajes largos (>400 chars)
│   ├── chats/                   # Historial SQLite de AGY (agy.go) y sesiones .jsonl de Claude (claude.go)
│   ├── config/                  # Carga de .env y resolución multiplataforma de binarios
│   ├── desktop/                 # Capturas Wayland (grim) y notificaciones nativas (Omarchy / Dunst / Windows)
│   ├── executor/                # Ejecución stream-json y kill recursivo de árbol de procesos (/proc)
│   ├── guard/                   # ChatGuard de inactividad, retención de prompts y caché 24h de uploads
│   ├── hooks/                   # Hooks Stop nativos en Go para AGY, Claude Code y notify-telegram
│   ├── models/                  # Catálogo de modelos (Gemini 4 Argon/Hydrogen, Claude, GPT) y detección de default CLI
│   ├── obsidian/                # Lectura de notas, guardado de PDFs en bóveda y generador de prompts Wicode
│   ├── projects/                # Listado de repositorios y lectura directa de rama Git (.git/HEAD)
│   ├── state/                   # Estado persistente atómico con sync.RWMutex y mapa Conversación <-> CWD
│   ├── telegram/                # Cliente HTTP nativo para Telegram Bot API (mensajes, edición, descarga y fotos)
│   ├── tokens/                  # Cuotas de IA en paralelo, fechas de reinicio, JSON de Waybar y panel Rofi
│   └── utils/                   # Formateo de uptime, sanitización y partición de Markdown en streaming
├── scripts/                     # Wrappers ligeros hacia subcomandos del binario telegram-bot
├── systemd/
│   └── agent.service            # Unidad systemd de usuario (con soporte Linger 24/7)
├── go.mod
└── README.md
```

---

## Comandos de Telegram

| Comando | Descripción |
| :--- | :--- |
| `/start`, `/status`, `/current` | Estado general, motor activo, chat, modelo efectivo, esfuerzo, proyecto (`cwd`) y uptime |
| `/model`, `/modelo` | Muestra el modelo real en uso (`settings.json` o seleccionado), esfuerzo, contexto y lista para cambiar |
| `/effort`, `/esfuerzo` | Cambia el nivel de razonamiento (`LOW`, `MEDIUM`, `HIGH`, `XHIGH`) |
| `/project`, `/cd <nombre>` | Cambia el directorio de trabajo (`cwd`) mediante botones o coincidencia rápida por nombre |
| `/resume`, `/chats`, `/historial` | Historial paginado (`◀️ / ▶️`) de conversaciones de AGY (SQLite) o Claude (`.jsonl`) con su proyecto asociado |
| `/new`, `/nuevo` | Prepara una conversación limpia para el siguiente mensaje |
| `/continue`, `/continuar` | Continúa la última conversación activa (`-c`) |
| `/agy` / `/claude` / `/engine` | Cambia al instante entre **Antigravity (AGY)** y **Claude Code** |
| `/tokens`, `/usage`, `/consumo` | Cuotas de IA con fechas exactas de reinicio, cuenta regresiva y botón de sincronización en vivo |
| `/obsidian`, `/prompts`, `/notas` | Explora notas `.md` de la bóveda de Obsidian y permite lanzarlas como prompt |
| `/config`, `/ajustes` | Activa o desactiva módulos en caliente (`ChatGuard`, `Buffer`, `Claude`, `Obsidian`, notificaciones) |
| `/screen`, `/ss`, `/captura` | Captura la pantalla actual (`grim`) y la envía al chat |
| `/stop`, `/cancel`, `/cancelar` | Mata inmediatamente el árbol completo de procesos de la tarea activa |

---

## Subcomandos CLI incluidos en el binario

El mismo binario `telegram-bot` reemplaza todos los scripts auxiliares de escritorio y hooks:

```bash
# Abrir panel Rofi de cuotas de IA (usado al hacer click en Waybar)
./telegram-bot dialog

# Imprimir JSON de cuotas (con caché rápida o consulta en vivo)
./telegram-bot stats --cached
./telegram-bot stats

# Hooks Stop de fin de turno (0 tokens, <2ms)
./telegram-bot hook agy
./telegram-bot hook claude

# Enviar notificación personalizada a Telegram desde terminal
./telegram-bot notify " Compilación lista" "El deploy terminó sin errores"
```

---

## Compilación y despliegue

```bash
# Ejecutar suite completa de tests
go test -v ./...

# Compilar binario único
go build -o telegram-bot ./cmd/bot

# Reiniciar servicio systemd de usuario
systemctl --user restart telegram-bot.service
```
