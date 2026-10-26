module studytracker

go 1.26.0

// Dependencies (modernc.org/sqlite, github.com/Microsoft/go-winio, golang.org/x/sys)
// are resolved by `go mod tidy`, which build.ps1 runs for you.

require (
	fyne.io/systray v1.12.2
	github.com/Microsoft/go-winio v0.6.2
	github.com/godbus/dbus/v5 v5.1.0
	github.com/jezek/xgb v1.3.1
	golang.org/x/sys v0.48.0
	modernc.org/sqlite v1.60.1
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	modernc.org/libc v1.77.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)
