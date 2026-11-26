#!/usr/bin/env bash
set -euo pipefail
export ANDROID_HOME="/home/paulobot/.local/share/mise/installs/android-sdk/23.0"
export ANDROID_SDK_ROOT="$ANDROID_HOME"
export PATH="/home/paulobot/.local/share/mise/installs/flutter/3.50.0/bin:$ANDROID_HOME/platform-tools:/home/paulobot/.local/share/mise/shims:$PATH"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PUBSPEC="$ROOT_DIR/mobile_app/pubspec.yaml"

BUILD_NUM=$(grep -E '^version:' "$PUBSPEC" | head -n1 | sed -E 's/.*\+([0-9]+).*/\1/')
if [[ -z "$BUILD_NUM" ]]; then
  echo "Error: no se pudo leer el build number de $PUBSPEC" >&2
  exit 1
fi

echo "Compilando APK con APP_BUILD=$BUILD_NUM..."
cd "$ROOT_DIR/mobile_app"
# Hub credentials come from the PC's .env (git-ignored), never from the source tree.
ENV_FILE="$ROOT_DIR/.env"
env_val() { [[ -f "$ENV_FILE" ]] && grep -E "^$1=" "$ENV_FILE" | tail -n1 | cut -d= -f2- | sed -E 's/^["'"'"']|["'"'"']$//g'; }
flutter build apk --release \
  --dart-define="APP_BUILD=$BUILD_NUM" \
  --dart-define="HUB_SECRET=$(env_val HUB_SECRET)" \
  --dart-define="GITHUB_GIST_TOKEN=$(env_val GITHUB_GIST_TOKEN)" \
  --dart-define="GITHUB_GIST_ID=$(env_val GITHUB_GIST_ID)"
