#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TUNNEL_PID=""

cleanup() {
  if [[ -n "$TUNNEL_PID" ]] && kill -0 "$TUNNEL_PID" 2>/dev/null; then
    kill "$TUNNEL_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT INT TERM

while true; do
  if tailscale status >/dev/null 2>&1; then
    if [[ -n "$TUNNEL_PID" ]] && kill -0 "$TUNNEL_PID" 2>/dev/null; then
      echo "Tailscale activo; deteniendo cloudflared ($TUNNEL_PID)..."
      kill "$TUNNEL_PID" 2>/dev/null || true
      wait "$TUNNEL_PID" 2>/dev/null || true
      TUNNEL_PID=""
    fi
  else
    if [[ -z "$TUNNEL_PID" ]] || ! kill -0 "$TUNNEL_PID" 2>/dev/null; then
      echo "Tailscale caído; iniciando tunnel-ntfy.sh..."
      "$ROOT_DIR/scripts/tunnel-ntfy.sh" &
      TUNNEL_PID=$!
    fi
  fi
  sleep 30
done
