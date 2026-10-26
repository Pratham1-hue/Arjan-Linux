#!/usr/bin/env bash
# Build Arjan for Linux and (re)start it in the tray.
#
#   ./build.sh              build, register the browser bridge, enable
#                           "Start at login", launch Arjan
#   ./build.sh --no-start   build only
#
# Everything ends up in dist/: arjan, nmhost, rules.json, web/.
set -euo pipefail
cd "$(dirname "$0")"

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "error: $1 not found — $2" >&2
    exit 1
  }
}
need go "install Go 1.24+ from https://go.dev/dl/"
need npm "install Node.js 20+ from https://nodejs.org/"

# Stop the running instance so the binary can be replaced.
if [ -x dist/arjan ]; then
  echo "==> stopping the running Arjan (if any)"
  ./dist/arjan --stop || true
  sleep 1
fi
# The browser keeps nmhost alive; stop it so it can be replaced
# (the extension reconnects automatically).
pkill -f 'dist/nmhost' 2>/dev/null || true

echo "==> resolving Go dependencies"
go mod tidy

echo "==> building the dashboard"
(
  cd dashboard
  [ -d node_modules ] || npm install
  npm run build
)

echo "==> staging files into dist/"
mkdir -p dist
rm -rf dist/web
cp -r dashboard/out dist/web
cp rules.json dist/rules.json

echo "==> building dist/arjan (tray app)"
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o dist/arjan ./cmd/daemon

echo "==> building dist/nmhost (browser bridge)"
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o dist/nmhost ./cmd/nmhost
chmod +x dist/arjan dist/nmhost

echo "==> registering the browser bridge (paths follow this folder)"
if [ -f .extension-id ] || [ -n "${EXTENSION_ID:-}" ]; then
  ./install-host.sh "${EXTENSION_ID:-}" || true
else
  echo "    skipped: no extension id yet."
  echo "    Load extension/ in chrome://extensions, then run: ./install-host.sh <ID>"
  echo "    (or save it once with:  echo <ID> > .extension-id  and re-run ./build.sh)"
fi

if [ "${1:-}" = "--no-start" ]; then
  echo
  echo "Built. Start Arjan with: ./dist/arjan"
  exit 0
fi

echo "==> enabling 'Start at login'"
./install-startup.sh

echo "==> starting Arjan in the system tray"
nohup ./dist/arjan >/dev/null 2>&1 &
sleep 1

cat <<'EOF'

Done. Look for the green ring icon in your tray (GNOME needs the
AppIndicator extension). Left-click it to open the dashboard,
right-click for the menu.

On Wayland only X11 windows are tracked; for complete tracking log in
with "Ubuntu on Xorg" (see the README).
EOF
