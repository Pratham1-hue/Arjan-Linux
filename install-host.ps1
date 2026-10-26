# Register com.studytracker as a Native Messaging host for Chrome and Edge (current user only).
# Safe to re-run any time - e.g. after moving the project folder: it rewrites
# the nmhost.exe path in the manifest to match the current location.
$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

$manifestPath = Join-Path $PWD "host\com.studytracker.json"
if (-not (Test-Path $manifestPath)) { throw "manifest not found: $manifestPath" }

$manifest = Get-Content $manifestPath -Raw | ConvertFrom-Json
$manifest.path = Join-Path $PWD "dist\nmhost.exe"
$manifest.description = "Arjan native messaging host - feeds the active browser tab to tracker.exe."
$manifest.allowed_origins = @($manifest.allowed_origins)
# Write UTF-8 without a BOM (browsers expect plain JSON).
[IO.File]::WriteAllText($manifestPath, ($manifest | ConvertTo-Json), (New-Object Text.UTF8Encoding $false))
Write-Host "manifest: nmhost path -> $($manifest.path)"

$keys = @(
  "HKCU:\Software\Google\Chrome\NativeMessagingHosts\com.studytracker",
  "HKCU:\Software\Microsoft\Edge\NativeMessagingHosts\com.studytracker"
)
foreach ($k in $keys) {
  New-Item -Path $k -Force | Out-Null
  Set-ItemProperty -Path $k -Name "(Default)" -Value $manifestPath
  Write-Host "registered: $k"
}

if ($manifest.allowed_origins -match "REPLACE_WITH_YOUR_EXTENSION_ID") {
  Write-Host ""
  Write-Host "IMPORTANT: put your extension ID (from chrome://extensions) into"
  Write-Host "host\com.studytracker.json, then run this script again."
}
Write-Host "Fully restart the browser for the change to take effect."
