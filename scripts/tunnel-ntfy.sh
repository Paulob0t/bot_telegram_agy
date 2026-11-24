#!/usr/bin/env bash
set -euo pipefail

TOPIC="${NTFY_TUNNEL_TOPIC:-hypragent-paulobot-tunnel}"
LOCAL_URL="${1:-http://127.0.0.1:8080}"

echo "Iniciando cloudflared hacia $LOCAL_URL (topic ntfy: $TOPIC)..."
cloudflared tunnel --url "$LOCAL_URL" 2>&1 | while IFS= read -r line; do
  echo "$line"
  if [[ "$line" =~ (https://[a-zA-Z0-9-]+\.trycloudflare\.com) ]]; then
    URL="${BASH_REMATCH[1]}"
    WSS_URL="wss://${URL#https://}/ws"
    echo "Publicando túnel en ntfy.sh/$TOPIC -> $WSS_URL"
    curl -fsS -d "$WSS_URL" "https://ntfy.sh/$TOPIC" >/dev/null || true
  fi
done
