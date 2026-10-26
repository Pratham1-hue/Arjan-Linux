# Arjan — Study & Laptop Usage Tracker

Arjan lives in your **system tray**, quietly records which apps, websites,
lectures and windows you spend time on, and shows you **where your day went**
and **what you actually learned** — on a local dashboard, one click away.

- 🟢 **Tray app** — starts with your desktop, no terminal, no `npm run dev`. ~25 MB RAM, ~0% CPU.
- 🧭 **Where your time went** — per app (VS Code, OpenCode, Gemini…), per website, and per
  activity ("*Linear Regression | Week 3 — YouTube*, 45 min").
- 🎓 **What you learned** — time per subject with topics, e.g.
  *"You learned Machine Learning for 1h 20m — mostly Gradient Descent and Linear Regression."*
- 🤖 **AI categorization (optional)** — uses Anthropic (Claude), Gemini (free key), OpenAI,
  OpenRouter, Groq, or a fully local Ollama to work out the subject of each lecture/page/window.
- 📊 **Focus Score**, category donut, day timeline, deep-work blocks, last-7-days chart.
- 🔔 **Notifications** — 9 PM daily report, and a nudge after 15 minutes of continuous distraction.
- 🔒 **Local-first** — everything stays on your laptop (AI is opt-in; see [Privacy](#privacy--security)).

![Arjan dashboard — focus score, today's split and the AI-written daily review](Screenshot_home_full.png)

## Screenshots

| | |
| --- | --- |
| **What you learned** — productive time per subject, with the pages and files behind it | **Apps & websites** — ranked, green when the time was productive |
| ![What you learned](Screenshot_home.png) | ![Apps and websites](Screenshot_apps_nd_website_usage.png) |
| **Timeline & last 7 days** — your day as a colored strip; click a bar to open that day | |
| ![Timeline and weekly chart](Screenshot_graph.png) | |

---

## Quick start

Runs on **Linux (Debian / Ubuntu)** and **Windows 10/11** from the same source; only the
build script, the binaries and the install steps differ.

**Needed once, to build:** [Go 1.24+](https://go.dev/dl/) and [Node.js 20+](https://nodejs.org/).
Distro packages are usually too old for both (Debian 12 ships Go 1.19 and Node 18), so
install the current releases — or use NodeSource for Node:

```bash
sudo apt install git build-essential
curl -fsSL https://deb.nodesource.com/setup_22.x | sudo -E bash - && sudo apt install -y nodejs
# Go: download the latest linux-amd64 tarball from https://go.dev/dl/ and put it in /usr/local/go
```

Only the built app runs day to day — not Node.

### 1. Build & launch — Linux (Debian / Ubuntu)

```bash
git clone <this repo> arjan && cd arjan
./build.sh                                             # build, register the bridge, start in the tray
```

`build.sh` builds the dashboard, compiles `dist/arjan` and `dist/nmhost`, registers the
browser bridge, enables **Start at login** and launches Arjan. **Moved the folder?** Run
`./build.sh` again — it re-points autostart and the bridge to the new path.

Look for the **green ring icon** in the tray (GNOME: the top-right menu — see
[tray on GNOME](#linux-notes)). Left-click opens the dashboard, right-click shows the menu.

### 2. Connect your browser (one time) — Linux

Without this, browsers are recorded only by window title. With it, Arjan sees the exact
page/lecture you're on.

1. Open `chrome://extensions` (or `edge://extensions`, `brave://extensions`) → turn on
   **Developer mode** → **Load unpacked** → select the `extension/` folder.
2. Copy the extension **ID** shown on that page.
3. Register the bridge with that ID:

   ```bash
   ./install-host.sh <EXTENSION_ID>      # writes the manifest for Chrome, Chromium, Brave, Edge
   echo <EXTENSION_ID> > .extension-id   # optional: so later runs need no argument
   ```

4. **Fully quit and restart** the browser (all windows).

### 3. Windows: build & launch

```powershell
cd "<path to>\Project Arjan"
powershell -ExecutionPolicy Bypass -File build.ps1
```

This builds everything into `dist\`, turns on **Start with Windows**, and starts Arjan.
**Moved the project folder?** Just run `build.ps1` again.

For the browser bridge on Windows, put your extension ID into
`host\com.studytracker.json` (replacing `REPLACE_WITH_YOUR_EXTENSION_ID`, keeping the
trailing `/`, one entry per browser in `allowed_origins`), then run
`powershell -ExecutionPolicy Bypass -File install-host.ps1` and fully restart the browser.

### 4. Turn on AI categorization (optional, recommended)

Dashboard → **⚙ Settings** → enable **AI categorization**:

1. Pick a provider:
   - **Google Gemini** — free tier, but only ~20 requests/day on flash models. Get a key at
     **<https://aistudio.google.com/apikey>**.
   - **Anthropic (Claude)** — paid per use (Haiku is cheapest). Key from
     **<https://platform.claude.com/settings/keys>**.
2. Paste the key in **API key** (one long token, no surrounding text).
3. Click **Test connection** — you should see a lecture classified as *Machine Learning*.
4. Click **Classify now** (after that it runs automatically every 5 minutes).

Hit Gemini's daily quota? Arjan pauses by itself until it resets (the banner says how long)
and you can switch providers any time — labels are kept.

Prefer total privacy? Install [Ollama](https://ollama.com), run `ollama pull llama3.2`,
and choose **Ollama (local, private)** — no key, nothing leaves your laptop.

---

## Using it

### Tray menu

| Item | What it does |
| --- | --- |
| *Today 3h 12m · Focus 75* | Live summary (updates every minute) |
| **Open Dashboard** | Opens / focuses the dashboard window (same as left-click) |
| **Pause tracking** | Stops recording until you resume (icon turns grey) |
| **Start with Windows** / **Start at login** | Toggle auto-start at sign-in |
| **Show today's report** | Today's summary as a notification |
| **Open data folder** | Database, logs, settings |
| **Quit Arjan** | Stops tracking and exits |

### Dashboard

| Section | Shows |
| --- | --- |
| **Focus** | Productive ÷ total tracked time (0–100) |
| **Today** | Productive / distraction / other split |
| **Daily review** | Your day in plain sentences — what you learned, apps, top site, distractions, deep work |
| **What you learned** | Productive time per subject, with topics (or lecture/page titles) |
| **By category** | Donut of categories |
| **Apps / Websites** | Ranked, colored by whether that time was productive |
| **What you did** | The specific lectures, pages and windows, longest first |
| **Timeline** | Your day as a colored strip (hover a block for details) + deep-work blocks (≥45 min uninterrupted productive) |
| **Last 7 days** | Stacked bars — **click a day** to open it |

Use **‹ ›** at the top to look at previous days. The **Tracking** pill pauses/resumes.

### Notifications

- **9:00 PM** — daily report: productive vs distraction time and your top subjects.
- **Nudge** — after 15 continuous minutes in a distraction (rules *or* AI-detected).

---

## Linux notes (Debian / Ubuntu)

### Xorg vs Wayland — please read

Reading the focused window requires X11. Arjan talks to the X server directly (no
`xdotool`/`xprintidle` needed), so:

| Session | App/window tracking | Idle detection | Browser tabs |
| --- | --- | --- | --- |
| **Xorg** (`Ubuntu on Xorg`) | ✅ everything | ✅ X server | ✅ |
| **Wayland** | ⚠️ only XWayland apps; native Wayland windows (GNOME Files, Terminal…) are invisible | ✅ via GNOME's idle monitor | ✅ (extension is independent) |

Ubuntu defaults to Wayland. For complete tracking, log out, click the little gear on the
login screen and pick **Ubuntu on Xorg** (once — it's remembered). Arjan logs a warning and
shows a one-time notification when it starts on Wayland.

### Tray icon

Arjan uses a StatusNotifierItem, the standard Linux tray protocol:

- **KDE Plasma, XFCE** (enable the *Status Tray* panel plugin), **Cinnamon, MATE, Budgie,
  LXQt** — works out of the box.
- **GNOME** — needs the AppIndicator extension:
  `sudo apt install gnome-shell-extension-appindicator` and enable *Ubuntu AppIndicators*
  (or **AppIndicator and KStatusNotifierItem Support**) in *Extensions*.

No tray (or a bare WM like i3/sway)? Arjan still runs — open `http://127.0.0.1:4000`
yourself, or run `arjan` again (it just focuses the dashboard).

### Notifications

`notify-send` (package `libnotify-bin`, installed by default on Ubuntu) is used for the daily
report and nudges; `kdialog` is the fallback, `zenity` for error dialogs.

### Browsers, Snap and Flatpak

Native messaging needs the browser to launch `dist/nmhost` with your real paths. That works
with **.deb** installs of Chrome, Chromium, Brave and Edge. **Snap and Flatpak builds are
sandboxed** and cannot see `~/.config/.../NativeMessagingHosts` or `$XDG_RUNTIME_DIR`, so the
page-level tracking silently stays off there — use the vendor `.deb` (or the distro package)
instead. Window titles are still recorded either way.

### Where things live on Linux

| Path | What |
| --- | --- |
| `~/.local/share/arjan/` | `study_logs.db`, `config.json`, `daemon.log`, `nmhost.log`, `crash.log` |
| `~/.config/autostart/arjan.desktop` | the *Start at login* entry (tray toggle writes/removes it) |
| `$XDG_RUNTIME_DIR/arjan.sock` | browser-bridge socket (mode 0600, per-user) |
| `dist/` inside the repo | built `arjan`, `nmhost`, `rules.json`, `web/` |

`STUDYTRACKER_DATA` overrides the data folder, `STUDYTRACKER_AI_KEY` overrides the stored key.

---

## How categorization works

Each activity is resolved in this order:

1. **`rules.json`** (yours, instant, offline). If a rule matches, its category and *kind*
   win.
2. **AI label** — for anything the rules don't cover (e.g. plain YouTube: a lecture is
   *productive*, a vlog is a *distraction*). For productive time the AI also adds a
   **subject** and **topic**, even when a rule matched (e.g. LeetCode → *Data Structures &
   Algorithms · Binary Search*).
3. Otherwise **Uncategorized** (counts as "other").

Labels are applied **retroactively**: edit a rule or turn on AI, and past days update too.

### `rules.json`

```json
{
  "categories": [
    { "name": "DSA Practice", "kind": "productive",  "match": ["leetcode.com"] },
    { "name": "Distractions", "kind": "distraction", "match": ["instagram.com", "youtube.com/shorts"] }
  ]
}
```

- `match` — case-insensitive text found in the **URL, window/tab title, or exe name**.
- `kind` — `productive`, `distraction`, or anything else (= neutral / "other").
- **First match wins**, so put specific rules first.
- Edit `rules.json` in the project folder, then copy it to `dist\` (or re-run `build.ps1`).
  Arjan reloads it within 15 seconds — no restart.

### AI details

- Each **distinct** activity (site + title, or app + window title) is sent **once**, in
  batches of 25, then cached forever in the database. Only activities with ≥1 minute in the
  last 7 days are classified. Steady state: a few requests per day, then none at all.
- Works with Anthropic's Messages API and with any OpenAI-compatible endpoint (Gemini,
  OpenAI, OpenRouter, Groq, Ollama, custom). Use **Load models** in Settings to pick a
  current model name.
- **Quotas**: free-tier limits are real (e.g. Gemini flash ≈ 20 requests/day). On a quota
  error Arjan parses the provider's reset time, **stops calling until then** (no hammering),
  and shows the wait in the dashboard. Switching providers in Settings resumes immediately.
- **Reset labels** in Settings re-classifies everything (e.g. after switching models).

---

## Privacy & security

- All tracking data stays on your laptop (`%LOCALAPPDATA%\StudyTracker\` on Windows,
  `~/.local/share/arjan/` on Linux).
- **When AI is on**, Arjan sends the provider only: app name, website **domain**, and
  window/tab **title** — never full URLs, never incognito tabs, never things `rules.json`
  already marks as distractions/neutral. Use **Ollama** to keep even that local.
- Your API key is encrypted with **Windows DPAPI** on Windows (only your account can decrypt
  it) and stored base64-encoded in `config.json` on Linux, a file only your user can read
  (`0600`). Set `STUDYTRACKER_AI_KEY` if you'd rather keep it out of the file entirely.
- The dashboard listens on `127.0.0.1` only, rejects non-localhost `Host` headers (DNS
  rebinding) and cross-site requests.
- Incognito windows: the extension is off there by default; if you enable it, only the word
  "Incognito" is stored.

---

## Architecture

```
 Chrome / Edge / Brave ──extension──► nmhost ──named pipe (Win) / Unix socket (Linux)──┐
                                                                                        ▼
 OS focus + idle (Win32 hook / X11 + Mutter) ────────────────────────────────────► arjan  (system tray)
                                                                                    ├─ SQLite: study_logs.db
                                                                                    ├─ rules.json + AI labels (optional, cached)
                                                                                    ├─ notifications (9 PM report, nudge)
                                                                                    └─ http://127.0.0.1:4000 ─► dashboard window
```

- **Windows**: no polling for window changes — a Win32 `SetWinEventHook` wakes Arjan only
  when you switch windows. **Linux/X11**: the focused window is sampled once per second
  (one property read; no measurable CPU), idle time comes from the X screen-saver counter
  or GNOME's idle monitor.
- A timer handles idle detection (3 min without input pauses logging) on both platforms.
- The dashboard is a static Next.js build served by the daemon — no Node at runtime.
  It opens as a chromeless Chrome/Chromium (or Edge) app window.

### Project layout

```
Project Arjan/
├── build.ps1              Windows: build everything into dist\ and (re)start Arjan
├── build.sh               Linux: the same, into dist/arjan + dist/nmhost
├── install-host.ps1       Windows: register the browser bridge (Chrome + Edge)
├── install-host.sh        Linux: register it for Chrome/Chromium/Brave/Edge
├── install-startup.ps1    Windows: enable Start with Windows (build.ps1 already does)
├── install-startup.sh     Linux: enable Start at login (build.sh already does)
├── rules.json             your categorization rules
├── cmd/daemon/            the tray app (Go)
│   ├── main.go              startup / shutdown wiring, CLI flags
│   ├── tray.go              tray icon & menu (shared)
│   ├── tray_windows.go      registry autostart, Edge app window, Explorer
│   ├── tray_linux.go        XDG autostart, --app window, xdg-open
│   ├── platform_windows.go  Win32: focus hook, idle time, windows, mutex
│   ├── platform_linux.go    X11 (xgb): focused window, idle; flock, data dirs
│   ├── server.go            local HTTP server + JSON API
│   ├── analyze.go           turns sessions into apps/sites/subjects/timeline
│   ├── ai.go                AI classifier (Anthropic + OpenAI-compatible)
│   ├── config.go            settings store (+ platform key storage)
│   ├── tracker.go           session tracking, idle, pause, distraction nudge
│   ├── store.go             SQLite schema & queries
│   ├── rules.go             rules.json engine
│   ├── scheduler.go         9 PM report
│   ├── toast_windows.go     PowerShell balloon notifications
│   ├── toast_linux.go       notify-send / kdialog notifications
│   └── pipe.go              browser-message protocol (+ pipe_windows/linux.go)
├── cmd/nmhost/            native-messaging bridge (Go)
├── extension/             Chrome/Edge/Brave extension (Manifest V3)
├── host/                  native-messaging host manifest template (Windows)
├── dashboard/             Next.js + Tailwind + Recharts (static export)
└── dist/                  built app: arjan + nmhost + rules.json + web/ (or .exe on Windows)
```

### Files Arjan creates

| File | Purpose |
| --- | --- |
| `study_logs.db` | All sessions + AI label cache (SQLite) |
| `config.json` | Settings (AI provider/model, stored key, port) |
| `daemon.log` | Activity log — check here first if something's off |
| `nmhost.log` | Browser bridge errors |
| `crash.log` | Written only if Arjan ever crashes |

Windows: `%LOCALAPPDATA%\StudyTracker\` · Linux: `~/.local/share/arjan/`.

### Command line (optional)

```bash
./dist/arjan                  # start in the tray (or open the dashboard if already running)
./dist/arjan --stop           # quit the running Arjan
./dist/arjan -report          # print today's report (add -v to also log to the terminal)
```

On Windows use `dist\tracker.exe` with the same flags (`-report | Out-Host` to see output).

---

## Development

```bash
# Go: tests (rules, analyzer, AI clients against fake servers, key storage, pause…)
go test ./cmd/daemon

# Cross-compile check for the other OS (both must stay clean):
GOOS=linux go build ./cmd/daemon ./cmd/nmhost
GOOS=windows go build ./cmd/daemon ./cmd/nmhost

# Dashboard with hot reload, using live data from the running tray app:
cd dashboard
npm run dev        # http://localhost:4001  (proxies /api to 127.0.0.1:4000)
```

After changing Go code or the dashboard, run `./build.sh` (Linux) or `build.ps1` (Windows)
again — it stops Arjan, rebuilds, and restarts it.

Platform code is separated by build tags (`//go:build windows` / `linux`), so a single
`go build ./...` works on both.

---

## Troubleshooting

| Problem | Fix |
| --- | --- |
| No tray icon | Windows: click `^` in the taskbar. Linux: GNOME needs the AppIndicator extension (see [tray](#tray-icon)); XFCE needs the *Status Tray* panel plugin. Check `daemon.log`. |
| Websites show only as "Google Chrome" | Browser bridge not set up — redo [step 2](#2-connect-your-browser-one-time--linux) and fully restart the browser; check `nmhost.log`. Snap/Flatpak browsers can't use the bridge — install the `.deb`. |
| Linux: most time shows as "Unknown app" | You're on a Wayland session. Log in with **Ubuntu on Xorg** (see [Xorg vs Wayland](#xorg-vs-wayland--please-read)). |
| Linux: `build.sh: permission denied` | Run `chmod +x build.sh install-host.sh install-startup.sh`. |
| Linux: nothing tracked after login | Check `~/.local/share/arjan/daemon.log` and that `~/.config/autostart/arjan.desktop` exists; toggle **Start at login** in the tray. |
| "What you learned" only shows rule categories | Turn on AI in Settings (or add rules). |
| AI error in the dashboard banner | Settings → **Test connection** shows the provider's message (bad key, wrong model → use **Load models**). |
| "quota exceeded … retry in 18h" | Gemini's free daily limit is used up. Wait for the reset, or switch to **Anthropic** (or any other provider) in Settings — existing labels are kept. |
| Dashboard says "isn't built yet" | Run `./build.sh` (Linux) or `build.ps1` (Windows). |
| Port 4000 already in use | Arjan picks a free port automatically; the tray icon always opens the right one. |
| Build can't overwrite the binary | Quit Arjan from the tray first (`./dist/arjan --stop`). |
| Stopped working after moving the folder | Re-run the build script (the tray menu toggles autostart and the bridge paths follow the new location). |

## Uninstall

Linux:

1. Tray icon → **Quit Arjan**, then uncheck **Start at login** (or
   `rm ~/.config/autostart/arjan.desktop`).
2. Remove the extension from `chrome://extensions`, and delete the manifests:
   `~/.config/{google-chrome,chromium,BraveSoftware/Brave-Browser,microsoft-edge}/NativeMessagingHosts/com.studytracker.json`.
3. `rm -rf ~/.local/share/arjan` (your data) and the project folder.

Windows:

1. Tray icon → **Quit Arjan**, and uncheck **Start with Windows** first (or
   `Remove-ItemProperty HKCU:\Software\Microsoft\Windows\CurrentVersion\Run -Name Arjan`).
2. Remove the extension from `chrome://extensions`, and delete the registry keys
   `HKCU\Software\Google\Chrome\NativeMessagingHosts\com.studytracker` and
   `HKCU\Software\Microsoft\Edge\NativeMessagingHosts\com.studytracker`.
3. Delete `%LOCALAPPDATA%\StudyTracker\` (your data) and the project folder.
