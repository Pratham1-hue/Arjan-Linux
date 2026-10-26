package main

import (
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestActivityIdentity(t *testing.T) {
	cases := []struct{ url, want string }{
		{"https://www.youtube.com/watch?v=x", "youtube.com"},
		{"http://127.0.0.1:4000/", "127.0.0.1:4000"},
		{"chrome://newtab/", "chrome://newtab"},
		{"", ""},
	}
	for _, c := range cases {
		if got := siteOf(c.url); got != c.want {
			t.Errorf("siteOf(%q) = %q, want %q", c.url, got, c.want)
		}
	}
	// Unread counters don't create a new activity.
	a := activityKey("chrome.exe", "https://www.youtube.com/watch?v=1", "(3) ML Lecture 4 - YouTube")
	b := activityKey("chrome.exe", "https://youtube.com/watch?v=1", "ML Lecture 4 - YouTube")
	if a != b {
		t.Errorf("keys differ: %q vs %q", a, b)
	}
	if appName("code.exe") != "VS Code" || appName("foo.exe") != "Foo" {
		t.Errorf("appName mapping broken")
	}
}

func TestDayReportWithAILabels(t *testing.T) {
	e := NewRuleEngine(writeRules(t, testRules))
	if err := e.Reload(); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Now()
	midnight, _ := dayBounds(now)
	lecture := "Linear Regression | Week 3 - YouTube"
	vlog := "My Goa Trip Vlog - YouTube"
	rows := []session{
		// 50 min lecture (plain YouTube: rules say Uncategorized, AI says ML)
		{start: midnight + 3600, end: midnight + 3600 + 3000, app: "chrome.exe", url: "https://www.youtube.com/watch?v=a", title: lecture},
		// 2 min gap, then 20 min of VS Code (rules: Web Dev) -> one deep work block
		{start: midnight + 6720, end: midnight + 7920, app: "code.exe", title: "stats.ts - dashboard - Visual Studio Code"},
		// 10 min vlog (AI says distraction)
		{start: midnight + 9000, end: midnight + 9600, app: "chrome.exe", url: "https://www.youtube.com/watch?v=b", title: vlog},
		// 5 min Gemini desktop app (rules: Productive Research)
		{start: midnight + 9600, end: midnight + 9900, app: "gemini.exe", title: "Gemini"},
	}
	for i := range rows {
		rows[i].category = e.Categorize(rows[i].app, rows[i].title, rows[i].url)
		if _, err := store.Insert(&rows[i]); err != nil {
			t.Fatal(err)
		}
	}

	labels := NewLabelCache()
	labels.Set(activityKey("chrome.exe", "https://www.youtube.com/watch?v=a", lecture),
		labelInfo{Kind: kindProductive, Category: "Lecture", Subject: "Machine Learning", Topic: "Linear Regression"})
	labels.Set(activityKey("chrome.exe", "https://www.youtube.com/watch?v=b", vlog),
		labelInfo{Kind: kindDistraction, Category: "Entertainment"})

	// The Gemini desktop app ("Gemini") matches no test rule and has no
	// label, so it stays Uncategorized and counts as "other".
	rep, err := NewAnalyzer(store, e, labels).Day(now)
	if err != nil {
		t.Fatal(err)
	}

	if rep.Productive != 3000+1200 || rep.Distraction != 600 || rep.Other != 300 {
		t.Fatalf("totals: productive=%d distraction=%d other=%d", rep.Productive, rep.Distraction, rep.Other)
	}
	if rep.FocusScore == nil || *rep.FocusScore != 82 {
		t.Fatalf("focus score = %v, want 82", rep.FocusScore)
	}

	if len(rep.BySubject) < 2 || rep.BySubject[0].Name != "Machine Learning" || rep.BySubject[0].Seconds != 3000 {
		t.Fatalf("subjects = %+v", rep.BySubject)
	}
	if rep.BySubject[0].Topics[0].Name != "Linear Regression" {
		t.Fatalf("topics = %+v", rep.BySubject[0].Topics)
	}
	if rep.BySubject[1].Name != "Web Dev" { // rules category is the fallback subject
		t.Fatalf("second subject = %q", rep.BySubject[1].Name)
	}

	if rep.ByApp[0].Name != "Google Chrome" || rep.ByApp[0].Seconds != 3600 {
		t.Fatalf("apps = %+v", rep.ByApp)
	}
	if len(rep.BySite) != 1 || rep.BySite[0].Site != "youtube.com" || rep.BySite[0].Kind != kindProductive {
		t.Fatalf("sites = %+v", rep.BySite)
	}
	if rep.Activities[0].Label != lecture || rep.Activities[0].Subject != "Machine Learning" {
		t.Fatalf("top activity = %+v", rep.Activities[0])
	}

	if len(rep.DeepWork) != 1 || rep.DeepWork[0].Seconds != 7920-3600 {
		t.Fatalf("deep work = %+v", rep.DeepWork)
	}
	if len(rep.Timeline) != 4 {
		t.Fatalf("timeline blocks = %d, want 4", len(rep.Timeline))
	}

	week, err := NewAnalyzer(store, e, labels).Week(now)
	if err != nil {
		t.Fatal(err)
	}
	if week[6].Productive != rep.Productive || week[6].Date != now.Format("2006-01-02") {
		t.Fatalf("week today = %+v", week[6])
	}
}

func TestParseLabels(t *testing.T) {
	inputs := []string{
		`{"items":[{"id":1,"kind":"Productive","category":"Lecture","subject":"Machine Learning","topic":"Gradient Descent"}]}`,
		"```json\n{\"items\":[{\"id\":\"1\",\"kind\":\"productive\",\"subject\":\"Machine Learning\"}]}\n```",
		`Sure! [{"id":1,"kind":"learning","subject":"Machine Learning"}]`,
	}
	for _, in := range inputs {
		items, err := parseLabels(in)
		if err != nil {
			t.Fatalf("parseLabels(%q): %v", in, err)
		}
		if len(items) != 1 || items[0].ID != 1 {
			t.Fatalf("parseLabels(%q) = %+v", in, items)
		}
		if l := items[0].label(); l.Kind != kindProductive || l.Subject != "Machine Learning" {
			t.Fatalf("label = %+v", l)
		}
	}
	if _, err := parseLabels("no json here"); err == nil {
		t.Fatal("expected an error for garbage input")
	}
	if normalizeKind("Distraction") != kindDistraction || normalizeKind("music") != kindNeutral {
		t.Fatal("normalizeKind broken")
	}
}

// Regression: logf once recursed into itself whenever a logger was
// set, crashing the app on the first pause.
func TestPauseResumeWithLogger(t *testing.T) {
	e := NewRuleEngine(writeRules(t, testRules))
	e.Reload()
	store, err := OpenStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var buf strings.Builder
	tr := NewTracker(store, e, nil, log.New(&buf, "", 0))
	var events []bool
	tr.OnPauseChange(func(p bool) { events = append(events, p) })

	tr.SetPaused(true)
	if !tr.Paused() {
		t.Fatal("not paused")
	}
	tr.OnBrowserTab("https://leetcode.com/", "LeetCode", false, "chrome.exe")
	tr.mu.Lock()
	cur := tr.cur
	tr.mu.Unlock()
	if cur != nil {
		t.Fatal("recorded activity while paused")
	}
	tr.SetPaused(false)
	tr.SetPaused(false) // no change: no extra event

	if len(events) != 2 || !events[0] || events[1] {
		t.Fatalf("pause events = %v", events)
	}
	if !strings.Contains(buf.String(), "tracking paused=true") {
		t.Fatalf("log = %q", buf.String())
	}
}

func TestTrayIconIsValidICO(t *testing.T) {
	ico := faviconICO(colorActive)
	if len(ico) < 6+16*4 || ico[0] != 0 || ico[2] != 1 || ico[4] != 4 {
		t.Fatalf("bad ICO header: % x", ico[:6])
	}
}
