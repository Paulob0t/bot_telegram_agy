#!/usr/bin/env bash
# Builds the hub (Termux) and fallback worker (proot Debian) for the phone,
# uploads them and restarts both tmux windows.
set -euo pipefail

CEL_HOST="${CEL_HOST:-u0_a274@192.168.1.73}"
CEL_PORT="${CEL_PORT:-8022}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$(mktemp -d)"
trap 'rm -rf "$OUT"' EXIT

ssh_cel() { ssh -p "$CEL_PORT" -o ConnectTimeout=5 -o BatchMode=yes "$CEL_HOST" "$@"; }

echo "Compilando..."
(cd "$ROOT" && CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build -o "$OUT/hub" ./cmd/bot)
(cd "$ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o "$OUT/worker" ./cmd/bot)

echo "Subiendo a $CEL_HOST..."
scp -q -P "$CEL_PORT" -o BatchMode=yes "$OUT/hub" "$CEL_HOST:agente-bot-android-arm64.new"
scp -q -P "$CEL_PORT" -o BatchMode=yes "$OUT/worker" "$CEL_HOST:agente-bot-worker.new"

echo "Reiniciando hub y worker..."
ssh_cel 'set -e
cp agente-bot-android-arm64 agente-bot-android-arm64.bak
mv -f agente-bot-android-arm64.new agente-bot-android-arm64
chmod +x agente-bot-android-arm64
proot-distro login debian -- bash -c "cp /usr/local/bin/agente-bot /usr/local/bin/agente-bot.bak && mv -f $HOME/agente-bot-worker.new /usr/local/bin/agente-bot && chmod 755 /usr/local/bin/agente-bot"
# Bracketed patterns keep pkill from matching this script. Killing the
# processes ends their tmux sessions; the boot script recreates the missing ones.
pkill -f "[m]ode=worker --role=cel" || true
pkill -f "[a]gente-bot-android-arm64 --mode=hub" || true
sleep 2
~/.termux/boot/start-services.sh
sleep 6
echo "hub: $(pgrep -fc "^[^ ]*agente-bot-android-arm64 --[m]ode=hub") proceso(s), worker: $(pgrep -fc "^agente-bot --[m]ode=worker --role=cel") proceso(s)"
tmux capture-pane -p -J -t cel_worker | grep -v "^$" | tail -1'
