# Build Arjan into dist\ and (re)start it in the tray.
#
#   powershell -ExecutionPolicy Bypass -File build.ps1            # build, enable autostart, launch
#   powershell -ExecutionPolicy Bypass -File build.ps1 -NoStart   # build only
param([switch]$NoStart)

$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

function Check($what) {
  if ($LASTEXITCODE -ne 0) { throw "$what failed (exit code $LASTEXITCODE)" }
}

if (Test-Path dist\tracker.exe) {
  Write-Host "==> stopping the running Arjan (if any)"
  & .\dist\tracker.exe --stop | Out-Null   # piping makes PowerShell wait
  Start-Sleep -Seconds 2
}
# The browser keeps nmhost.exe open; stop it so it can be replaced
# (the extension reconnects automatically).
Stop-Process -Name nmhost -Force -ErrorAction SilentlyContinue

Write-Host "==> resolving Go dependencies"
go mod tidy; Check "go mod tidy"

Write-Host "==> building the dashboard"
Push-Location dashboard
try {
  if (-not (Test-Path node_modules)) { npm install; Check "npm install" }
  npm run build; Check "dashboard build"
} finally { Pop-Location }

Write-Host "==> staging files into dist\"
New-Item -ItemType Directory -Force -Path dist | Out-Null
if (Test-Path dist\web) { Remove-Item dist\web -Recurse -Force }
Copy-Item dashboard\out dist\web -Recurse
Copy-Item rules.json dist\rules.json -Force

Write-Host "==> building tracker.exe (tray app, no console window)"
go build -ldflags "-H windowsgui -s -w" -o dist\tracker.exe ./cmd/daemon; Check "tracker build"

Write-Host "==> building nmhost.exe (browser bridge)"
go build -ldflags "-s -w" -o dist\nmhost.exe ./cmd/nmhost; Check "nmhost build"
# If the browser reconnected during the build it's running the old
# bridge; stop it so the next reconnect uses the new one.
Stop-Process -Name nmhost -Force -ErrorAction SilentlyContinue

Write-Host "==> registering the browser bridge (paths follow this folder)"
& .\install-host.ps1 | Out-Null

if (-not $NoStart) {
  Write-Host "==> enabling 'Start with Windows'"
  $exe = (Resolve-Path dist\tracker.exe).Path
  Set-ItemProperty -Path "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run" -Name "Arjan" -Value "`"$exe`""
  # Remove the old Task Scheduler entry from earlier versions, if present.
  Unregister-ScheduledTask -TaskName StudyTracker -Confirm:$false -ErrorAction SilentlyContinue

  Write-Host "==> starting Arjan in the system tray"
  Start-Process -FilePath dist\tracker.exe
}

Write-Host ""
Write-Host "Done. Look for the green ring icon in the system tray (click ^ if it's hidden)."
Write-Host "Left-click it to open the dashboard, right-click for the menu."
