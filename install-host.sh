#!/usr/bin/env bash
# Register com.studytracker as a Native Messaging host for the Chromium
# browsers of the current user (Chrome, Chromium, Brave, Edge, Vivaldi).
#
#   ./install-host.sh <EXTENSION_ID>
#
# Find the id at chrome://extensions (Developer mode → "ID"). To avoid
# passing it every time, save it once:  echo <ID> > .extension-id
#
# Safe to re-run any time — e.g. after moving the project folder: it
# rewrites the path in every manifest to match the current location.
set -euo pipefail
cd "$(dirname "$0")"

HOST=com.studytracker
NMHOST="$(pwd)/dist/nmhost"

if [ ! -x "$NMHOST" ]; then
  echo "error: not built yet: $NMHOST (run ./build.sh first)" >&2
  exit 1
fi

EXT_ID="${1:-}"
if [ -z "$EXT_ID" ] && [ -f .extension-id ]; then
  EXT_ID="$(tr -d '[:space:]' < .extension-id)"
fi
if [ -z "$EXT_ID" ]; then
  cat >&2 <<'EOF'
usage: ./install-host.sh <EXTENSION_ID>

Load the extension first:
  1. chrome://extensions → enable "Developer mode"
  2. "Load unpacked" → choose the extension/ folder
  3. copy the extension ID and pass it here
EOF
  exit 1
fi

# Unpacked Chrome extension ids are 32 chars a-p. Catch pasted URLs.
if ! [[ "$EXT_ID" =~ ^[a-p]{32}$ ]]; then
  echo "warning: '$EXT_ID' doesn't look like an extension id (32 chars a-p)." >&2
  echo "         use the bare id from chrome://extensions, not a URL." >&2
fi

MANIFEST="$(mktemp)"
trap 'rm -f "$MANIFEST"' EXIT
cat > "$MANIFEST" <<JSON
{
  "name": "$HOST",
  "description": "Arjan native messaging host - feeds the active browser tab to the Arjan daemon.",
  "path": "$NMHOST",
  "type": "stdio",
  "allowed_origins": ["chrome-extension://$EXT_ID/"]
}
JSON

BROWSER_DIRS=(
  "$HOME/.config/google-chrome"
  "$HOME/.config/chromium"
  "$HOME/.config/BraveSoftware/Brave-Browser"
  "$HOME/.config/microsoft-edge"
  "$HOME/.config/vivaldi"
  "$HOME/.config/opera"
)

installed=0
for dir in "${BROWSER_DIRS[@]}"; do
  # Only touch browsers that exist, but always create Chrome's (the
  # usual case, and a missing manifest is otherwise silent).
  if [ "$dir" != "$HOME/.config/google-chrome" ] && [ ! -d "$dir" ]; then
    continue
  fi
  mkdir -p "$dir/NativeMessagingHosts"
  install -m 0644 "$MANIFEST" "$dir/NativeMessagingHosts/$HOST.json"
  echo "registered: $dir/NativeMessagingHosts/$HOST.json"
  installed=$((installed + 1))
done

if [ "$installed" -eq 0 ]; then
  echo "error: no browser config directories found under ~/.config" >&2
  exit 1
fi

cat <<'EOF'

Fully quit and restart the browser (all windows) for the change to
take effect. Snap/Flatpak browsers are sandboxed and cannot use native
messaging — install Chrome/Chromium/Brave/Edge from their .deb instead.
EOF
