// Command tracker is Arjan: a tray app that tracks what you work
// on, categorizes it (rules.json + optional AI), and serves the
// dashboard at http://127.0.0.1:4000.
//
//	tracker            start in the tray (opens the dashboard if already running)
//	tracker --stop     ask the running app to quit cleanly
//	tracker -report    show today's report now
//	tracker -db x.db   use a different SQLite file
//	tracker -rules r.json  use a different rules file
//	tracker -v         also log to the console
//
// Data, logs and settings live in %LOCALAPPDATA%\StudyTracker\ on
// Windows and ~/.local/share/arjan/ on Linux; rules.json and the
// dashboard (web\) sit next to the executable.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"

	"fyne.io/systray"
)

func main() {
	attachParentConsole()

	var (
		dbFlag    = flag.String("db", "", "SQLite database path (default: the per-user data folder)")
		rulesFlag = flag.String("rules", "", "rules.json path (default: rules.json next to the executable)")
		verbose   = flag.Bool("v", false, "also log to the console")
		stop      = flag.Bool("stop", false, "ask a running Arjan to exit, then quit")
		report    = flag.Bool("report", false, "show today's daily report now, then quit")
	)
	flag.Parse()

	if *stop {
		code := sendStop()
		if code == 0 {
			fmt.Println("Arjan is stopping.")
		} else {
			fmt.Println("Arjan is not running.")
		}
		os.Exit(code)
	}

	dir := dataDir()
	// The folder holds the database, logs and your settings: keep it
	// private to your user.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fatalf("create data dir: %v", err)
	}
	dbPath := *dbFlag
	if dbPath == "" {
		dbPath = filepath.Join(dir, "study_logs.db")
	}
	cfg, err := LoadConfig(filepath.Join(dir, "config.json"))
	if err != nil {
		fatalf("read config.json: %v", err)
	}

	if *report {
		reportNow(*rulesFlag, dbPath)
		return
	}

	if !acquireSingleInstance() {
		// Already running in the tray: just show the dashboard.
		openDashboardURL(fmt.Sprintf("http://127.0.0.1:%d", cfg.Get().Port))
		return
	}

	// A tray app has no console: send any fatal error / unrecovered
	// panic to crash.log instead of losing it.
	if f, err := os.OpenFile(filepath.Join(dir, "crash.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		_ = debug.SetCrashOutput(f, debug.CrashOptions{})
	}

	logFile, err := os.OpenFile(filepath.Join(dir, "daemon.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fatalf("open log file: %v", err)
	}
	defer logFile.Close()
	var sink io.Writer = logFile
	if *verbose {
		sink = io.MultiWriter(logFile, os.Stderr)
	}
	lg := log.New(sink, "tracker ", log.LstdFlags)
	// The tray and X11 libraries log through the standard logger; keep
	// their messages in daemon.log instead of losing them (a missing
	// tray icon shows up there).
	log.SetOutput(sink)
	logPlatformNotes(lg)

	store, err := OpenStore(dbPath)
	if err != nil {
		fatalf("open database: %v", err)
	}

	engine := NewRuleEngine(resolveRulesPath(*rulesFlag))
	if err := engine.Reload(); err != nil {
		lg.Printf("warning: rules not loaded from %s: %v (everything stays Uncategorized)", engine.Path(), err)
	}

	labels := NewLabelCache()
	if m, err := store.Labels(); err == nil {
		labels.Replace(m)
	} else {
		lg.Printf("load AI labels: %v", err)
	}

	notifier := func(title, message string) {
		if err := notifyToast(title, message); err != nil {
			lg.Printf("toast: %v", err)
		}
	}

	analyzer := NewAnalyzer(store, engine, labels)
	tracker := NewTracker(store, engine, notifier, lg)
	tracker.labels = labels
	classifier := NewClassifier(store, engine, labels, cfg, lg)

	exeDir := "."
	if exe, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exe)
	}
	server := &Server{
		analyzer: analyzer, tracker: tracker, cfg: cfg, ai: classifier,
		webDir: filepath.Join(exeDir, "web"), dataDir: dir, rulesPath: engine.Path(),
		icon: faviconICO(colorActive), lg: lg,
	}
	if err := server.Start(); err != nil {
		fatalf("start dashboard server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-sig:
			lg.Printf("signal received, shutting down")
			cancel()
		case <-ctx.Done():
		}
	}()

	// Event-driven foreground-window hook (0% CPU while waiting).
	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		if err := runEventLoop(tracker.OnForeground); err != nil {
			lg.Printf("event loop: %v", err)
			cancel()
		}
	}()

	// Local socket that the nmhost bridge forwards browser tabs to.
	pipeDone := make(chan struct{})
	go func() {
		defer close(pipeDone)
		if err := serveBrowserPipe(ctx, tracker, func() {
			cancel()
		}, lg); err != nil {
			lg.Printf("browser pipe: %v", err)
		}
	}()

	trackerDone := make(chan struct{})
	go func() { defer close(trackerDone); tracker.Run(ctx) }()

	schedDone := make(chan struct{})
	go func() { defer close(schedDone); runScheduler(ctx, analyzer, notifier, lg) }()

	aiDone := make(chan struct{})
	go func() { defer close(aiDone); classifier.Run(ctx) }()

	lg.Printf("Arjan started (db=%s, rules=%s)", dbPath, engine.Path())

	tray := &Tray{
		tracker: tracker, analyzer: analyzer, server: server,
		notifier: notifier, dataDir: dir, lg: lg, quit: cancel,
	}
	// Quit the tray when anything else ends the app (--stop, signal).
	go func() { <-ctx.Done(); systray.Quit() }()
	systray.Run(func() {
		tray.onReady()
		if ctx.Err() != nil { // stop requested during startup
			systray.Quit()
		}
	}, func() {})

	lg.Printf("shutting down")
	cancel()
	server.Shutdown()
	<-trackerDone
	<-schedDone
	<-aiDone
	<-pipeDone
	tracker.Shutdown() // close the open session + database
	stopEventLoop()
	<-loopDone
	lg.Printf("stopped")
}

// fatalf reports a startup error. There is no console in tray mode,
// so it is shown in a message box (and printed, for CLI use).
func fatalf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(os.Stderr, "Arjan:", msg)
	messageBox("Arjan", msg)
	os.Exit(1)
}

// reportNow computes and shows the daily report immediately.
func reportNow(rulesFlag, dbPath string) {
	engine := NewRuleEngine(resolveRulesPath(rulesFlag))
	if err := engine.Reload(); err != nil {
		log.Printf("warning: rules not loaded from %s: %v", engine.Path(), err)
	}
	store, err := OpenStore(dbPath)
	if err != nil {
		fatalf("open database: %v", err)
	}
	defer store.Close()

	labels := NewLabelCache()
	if m, err := store.Labels(); err == nil {
		labels.Replace(m)
	}
	msg, err := dailyReport(NewAnalyzer(store, engine, labels))
	if err != nil {
		fatalf("daily report: %v", err)
	}
	fmt.Println(msg)
	if err := notifyToast("Arjan Daily Report", msg); err != nil {
		log.Printf("toast: %v", err)
	}
}

// resolveRulesPath picks rules.json: the -rules flag wins;
// then rules.json next to the executable (build.ps1 copies
// it there); then the working directory.
func resolveRulesPath(flagPath string) string {
	if flagPath != "" {
		return flagPath
	}
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), "rules.json")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "rules.json"
}

// dataDir is where the database, logs and settings live. Override
// with STUDYTRACKER_DATA if you want them somewhere else.
func dataDir() string {
	if v := os.Getenv("STUDYTRACKER_DATA"); v != "" {
		return v
	}
	return platformDataDir()
}
