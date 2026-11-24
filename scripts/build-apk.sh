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
flutter build apk --release --dart-define="APP_BUILD=$BUILD_NUM"
