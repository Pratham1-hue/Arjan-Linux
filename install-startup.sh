#!/usr/bin/env bash
# Make Arjan start in the tray when you log in.
# (build.sh already does this; the tray menu has a "Start at login"
# toggle too.)
set -euo pipefail
cd "$(dirname "$0")"

BIN="$(pwd)/dist/arjan"
if [ ! -x "$BIN" ]; then
  echo "error: not built yet: $BIN (run ./build.sh first)" >&2
  exit 1
fi

AUTOSTART_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/autostart"
ENTRY="$AUTOSTART_DIR/arjan.desktop"
mkdir -p "$AUTOSTART_DIR"

cat > "$ENTRY" <<DESKTOP
[Desktop Entry]
Type=Application
Name=Arjan
Comment=Study tracker
Exec="$BIN"
Terminal=false
NoDisplay=true
X-GNOME-Autostart-enabled=true
DESKTOP

echo "Arjan will start at every login ($ENTRY)."
echo "To undo: tray icon → uncheck \"Start at login\", or delete that file."
