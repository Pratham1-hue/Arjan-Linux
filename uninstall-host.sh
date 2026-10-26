#!/usr/bin/env bash
# Remove the native-messaging manifests installed by install-host.sh.
set -euo pipefail

HOST=com.studytracker
BROWSER_DIRS=(
  "$HOME/.config/google-chrome"
  "$HOME/.config/chromium"
  "$HOME/.config/BraveSoftware/Brave-Browser"
  "$HOME/.config/microsoft-edge"
  "$HOME/.config/vivaldi"
  "$HOME/.config/opera"
)

for dir in "${BROWSER_DIRS[@]}"; do
  manifest="$dir/NativeMessagingHosts/$HOST.json"
  if [ -f "$manifest" ]; then
    rm -f "$manifest"
    echo "removed: $manifest"
  fi
done

echo "Restart the browser to drop the bridge."
