# HyprAgent (`agente-bot`): núcleo Go + app móvil para Antigravity (AGY) y Claude Code

Servidor en Go que controla **Antigravity CLI (`agy`)** y **Claude Code (`claude`)** desde la app Flutter **HyprAgent** (`mobile_app/`). La app se conecta directo por Tailscale o a través de un Hub relay descubierto por Gist.

---

## Características

- **Doble motor en streaming (`stream-json`)**: alterna entre `agy` y `claude` mostrando herramientas, comandos y archivos editados en tiempo real.
- **Sesiones por proyecto**: Nuevo chat → proyecto → retomar un chat de ese proyecto o empezar uno limpio.
- **Aprobación de herramientas desde el móvil**: con `mobile_approvals` activo, `Bash`/`Write`/`Edit` de Claude esperan tu OK en la app (hook `PreToolUse`).
- **Cancelación del árbol de procesos (`/proc`)**: `/stop` mata subprocesos nietos aunque creen su propio `setsid`.
- **Archivos desde la app**: caché 24h en `~/.cache/telegram-paulobot/uploads/`, guardado en el proyecto, `~/Downloads` u Obsidian.
- **Cuotas de IA**: Claude Code y Antigravity con hora exacta de reinicio, en `/tokens`, Waybar y `rofi`.
- **Notificaciones**: los hooks Stop de AGY y Claude avisan a la app al terminar una tarea.

---

## Estructura

```text
agente-bot/
├── cmd/bot/main.go              # Entrypoint: modos (servidor, hub, worker) y subcomandos CLI
├── mobile_app/                  # App Flutter HyprAgent
├── internal/
│   ├── agent/                   # Servicio de ejecución, eventos y aprobaciones pendientes
│   ├── chats/                   # Historial de AGY (SQLite) y sesiones de Claude (.jsonl)
│   ├── config/                  # Carga de .env y rutas por plataforma
│   ├── desktop/                 # Capturas y notificaciones de escritorio
│   ├── executor/                # Ejecución stream-json y kill del árbol de procesos
│   ├── hooks/                   # Hooks Stop (agy, claude) y PreToolUse (claude-perm)
│   ├── models/                  # Catálogo de modelos y modelo efectivo
│   ├── obsidian/                # Bóveda: lectura de notas y guardado de PDFs
│   ├── projects/                # Listado de proyectos y rama Git
│   ├── relay/                   # Hub relay y worker (descubrimiento por Gist)
│   ├── state/                   # Estado persistente y features activables
│   ├── tokens/                  # Cuotas de IA, Waybar y panel rofi
│   ├── transport/mobile/        # Servidor WebSocket/HTTP directo para la app
│   ├── uploads/                 # Carpeta temporal de archivos recibidos
│   └── utils/                   # Formateo y sanitización de Markdown
├── scripts/                     # Wrappers hacia subcomandos del binario y build del APK
├── systemd/agent.service        # Unidad systemd de usuario
└── Makefile
```

---

## Comandos desde la app

| Comando | Descripción |
| :--- | :--- |
| `/status` | Motor, modelo, esfuerzo y proyecto activo |
| `/new`, `/nuevo` | Nuevo chat: elige proyecto y luego retoma o empieza limpio |
| `/model` | Selector de modelo |
| `/resume`, `/chats` | Chats del proyecto activo |
| `/agy`, `/claude` | Cambia de motor |
| `/approvals` | Activa o desactiva la aprobación de herramientas desde el móvil |
| `/tokens`, `/usage` | Cuotas de IA |
| `/scan` | Escanea proyectos y CLIs de la PC |
| `/stop`, `/cancel` | Detiene la tarea activa |

---

## Subcomandos CLI

```bash
./agente-bot dialog              # Panel rofi de cuotas (click en Waybar)
./agente-bot stats [--cached]    # JSON de cuotas
./agente-bot hook agy            # Hook Stop de Antigravity
./agente-bot hook claude         # Hook Stop de Claude Code
./agente-bot hook claude-perm    # Hook PreToolUse: aprobación desde el móvil
./agente-bot notify "Título" "cuerpo"   # Notificación manual a la app
./agente-bot mobile-info         # URL y token de conexión directa
./agente-bot -mode hub|worker    # Hub relay o worker
```

---

## Configuración (`.env`)

| Variable | Uso |
| :--- | :--- |
| `MOBILE_WS_TOKEN` | Obligatoria. Token de la conexión directa y de los hooks |
| `HUB_SECRET`, `GITHUB_GIST_TOKEN`, `GITHUB_GIST_ID` | Hub relay. `scripts/build-apk.sh` también los inyecta en el APK |

Los hooks se ejecutan desde `~/.local/bin` dentro de cualquier proyecto y leen `~/.config/agente-bot/.env`; `make install` crea ese enlace.

---

## Compilación y despliegue

```bash
make test                               # go test ./...
go build -o agente-bot ./cmd/bot        # binario del servicio
make install                            # binario de los hooks + enlace del .env
systemctl --user restart agent.service
./scripts/build-apk.sh                  # APK release
```
