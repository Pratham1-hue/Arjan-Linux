package main

import (
	"fmt"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// ------------------------------------------------------------ AI labels ---

// labelInfo is what the AI says about one distinct activity.
type labelInfo struct {
	Kind     string `json:"kind"`
	Category string `json:"category"`
	Subject  string `json:"subject"`
	Topic    string `json:"topic"`
}

// LabelCache is an in-memory copy of the ai_labels table. All
// methods are nil-safe so tests can run without one.
type LabelCache struct {
	mu sync.RWMutex
	m  map[string]labelInfo
}

func NewLabelCache() *LabelCache { return &LabelCache{m: map[string]labelInfo{}} }

func (c *LabelCache) Get(key string) (labelInfo, bool) {
	if c == nil {
		return labelInfo{}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	l, ok := c.m[key]
	return l, ok
}

func (c *LabelCache) Set(key string, l labelInfo) {
	c.mu.Lock()
	c.m[key] = l
	c.mu.Unlock()
}

func (c *LabelCache) Replace(m map[string]labelInfo) {
	c.mu.Lock()
	c.m = m
	c.mu.Unlock()
}

func (c *LabelCache) Len() int {
	if c == nil {
		return 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.m)
}

// ------------------------------------------------------ activity identity ---

// notifPrefix matches unread counters like "(3) " that sites
// prepend to tab titles.
var notifPrefix = regexp.MustCompile(`^\(\d+\+?\)\s*`)

func normTitle(t string) string {
	return strings.TrimSpace(notifPrefix.ReplaceAllString(strings.TrimSpace(t), ""))
}

// siteOf turns a URL into a display domain: "youtube.com",
// "127.0.0.1:4000", "chrome://newtab".
func siteOf(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	switch u.Scheme {
	case "http", "https":
		return strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	case "file":
		return "local file"
	case "":
		return ""
	default:
		return u.Scheme + "://" + u.Host
	}
}

// activityKey identifies "the same thing" across sessions:
// the same page title on the same site, or the same window
// title in the same app. AI labels are cached per key.
func activityKey(app, rawURL, title string) string {
	t := strings.ToLower(normTitle(title))
	if rawURL != "" || browserExes[app] {
		return "web|" + siteOf(rawURL) + "|" + t
	}
	return "app|" + strings.ToLower(app) + "|" + t
}

var appNames = map[string]string{
	// --- Windows ---
	"chrome.exe":               "Google Chrome",
	"msedge.exe":               "Microsoft Edge",
	"brave.exe":                "Brave",
	"firefox.exe":              "Firefox",
	"code.exe":                 "VS Code",
	"cursor.exe":               "Cursor",
	"windsurf.exe":             "Windsurf",
	"opencode.exe":             "OpenCode",
	"gemini.exe":               "Gemini",
	"claude.exe":               "Claude",
	"chatgpt.exe":              "ChatGPT",
	"pycharm64.exe":            "PyCharm",
	"idea64.exe":               "IntelliJ IDEA",
	"explorer.exe":             "File Explorer",
	"notepad.exe":              "Notepad",
	"windowsterminal.exe":      "Windows Terminal",
	"powershell.exe":           "PowerShell",
	"pwsh.exe":                 "PowerShell",
	"cmd.exe":                  "Command Prompt",
	"spotify.exe":              "Spotify",
	"discord.exe":              "Discord",
	"whatsapp.exe":             "WhatsApp",
	"telegram.exe":             "Telegram",
	"slack.exe":                "Slack",
	"ms-teams.exe":             "Microsoft Teams",
	"teams.exe":                "Microsoft Teams",
	"zoom.exe":                 "Zoom",
	"winword.exe":              "Word",
	"excel.exe":                "Excel",
	"powerpnt.exe":             "PowerPoint",
	"onenote.exe":              "OneNote",
	"acrobat.exe":              "Adobe Acrobat",
	"acrord32.exe":             "Adobe Acrobat",
	"obsidian.exe":             "Obsidian",
	"notion.exe":               "Notion",
	"vlc.exe":                  "VLC",
	"steam.exe":                "Steam",
	"searchhost.exe":           "Windows Search",
	"applicationframehost.exe": "Windows App",

	// --- Linux (the name comes from /proc/<pid>/exe) ---
	"chrome":                "Google Chrome",
	"google-chrome":         "Google Chrome",
	"google-chrome-stable":  "Google Chrome",
	"chromium":              "Chromium",
	"chromium-browser":      "Chromium",
	"brave":                 "Brave",
	"brave-browser":         "Brave",
	"microsoft-edge":        "Microsoft Edge",
	"msedge":                "Microsoft Edge",
	"firefox":               "Firefox",
	"code":                  "VS Code",
	"code-insiders":         "VS Code",
	"cursor":                "Cursor",
	"windsurf":              "Windsurf",
	"opencode":              "OpenCode",
	"pycharm":               "PyCharm",
	"pycharm-community":     "PyCharm",
	"idea":                  "IntelliJ IDEA",
	"idea-community":        "IntelliJ IDEA",
	"nautilus":              "Files",
	"dolphin":               "Dolphin",
	"thunar":                "Thunar",
	"nemo":                  "Files",
	"gedit":                 "Text Editor",
	"gnome-text-editor":     "Text Editor",
	"kate":                  "Kate",
	"gnome-terminal-server": "Terminal",
	"gnome-terminal":        "Terminal",
	"konsole":               "Konsole",
	"xterm":                 "Terminal",
	"alacritty":             "Alacritty",
	"kitty":                 "Kitty",
	"wezterm":               "WezTerm",
	"tilix":                 "Tilix",
	"terminator":            "Terminator",
	"bash":                  "Terminal",
	"zsh":                   "Terminal",
	"fish":                  "Terminal",
	"spotify":               "Spotify",
	"discord":               "Discord",
	"telegram-desktop":      "Telegram",
	"slack":                 "Slack",
	"teams-for-linux":       "Microsoft Teams",
	"zoom":                  "Zoom",
	"libreoffice":           "LibreOffice",
	"soffice":               "LibreOffice",
	"soffice.bin":           "LibreOffice",
	"evince":                "Document Viewer",
	"okular":                "Okular",
	"zathura":               "Zathura",
	"obsidian":              "Obsidian",
	"notion-app":            "Notion",
	"vlc":                   "VLC",
	"mpv":                   "mpv",
	"rhythmbox":             "Rhythmbox",
	"obs":                   "OBS Studio",
	"gimp":                  "GIMP",
	"kdenlive":              "Kdenlive",
	"steam":                 "Steam",
	"thunderbird":           "Thunderbird",
	"gnome-shell":           "GNOME Shell",
	"plasmashell":           "KDE Plasma",
	"arjan":                 "Arjan",
	"tracker":               "Arjan",
}

// appName is a friendly name for an executable.
func appName(exe string) string {
	exe = strings.ToLower(exe)
	if n, ok := appNames[exe]; ok {
		return n
	}
	n := strings.TrimSuffix(exe, ".exe")
	if n == "" || n == "unknown" {
		return "Unknown app"
	}
	return strings.ToUpper(n[:1]) + n[1:]
}

// --------------------------------------------------------------- analyzer ---

// Analyzer turns raw sessions into dashboard / report data.
// Category and kind come from rules.json first (fast, offline,
// your choice wins); AI labels fill in anything the rules don't
// cover, and add a subject and topic to productive time.
type Analyzer struct {
	store  *Store
	rules  *RuleEngine
	labels *LabelCache
}

func NewAnalyzer(store *Store, rules *RuleEngine, labels *LabelCache) *Analyzer {
	return &Analyzer{store: store, rules: rules, labels: labels}
}

type resolved struct {
	Key, Category, Kind, Subject, Topic string
	App, Site, Label                    string
}

func (a *Analyzer) resolve(s sessionRow) resolved {
	r := resolved{
		Key:   activityKey(s.App, s.URL, s.Title),
		App:   appName(s.App),
		Site:  siteOf(s.URL),
		Label: normTitle(s.Title),
	}
	if r.Label == "" {
		r.Label = r.Site
	}
	if r.Label == "" {
		r.Label = r.App
	}

	lbl, hasLbl := a.labels.Get(r.Key)
	switch cat := a.rules.Categorize(s.App, s.Title, s.URL); {
	case cat != uncategorized:
		r.Category = cat
		r.Kind = a.rules.KindOf(cat)
		if r.Kind == kindProductive && hasLbl {
			r.Subject, r.Topic = lbl.Subject, lbl.Topic
		}
	case hasLbl:
		r.Category = lbl.Category
		if r.Category == "" {
			r.Category = uncategorized
		}
		r.Kind = lbl.Kind
		if r.Kind == kindProductive {
			r.Subject, r.Topic = lbl.Subject, lbl.Topic
		}
	default:
		r.Category = uncategorized
		r.Kind = kindNeutral
	}
	if r.Kind == kindProductive && r.Subject == "" {
		r.Subject = r.Category
	}
	return r
}

// ---------------------------------------------------------- report types ---

type CategoryStat struct {
	Name    string `json:"name"`
	Seconds int64  `json:"seconds"`
	Kind    string `json:"kind"`
}

type NamedSecs struct {
	Name    string `json:"name"`
	Seconds int64  `json:"seconds"`
}

type SubjectStat struct {
	Name    string      `json:"name"`
	Seconds int64       `json:"seconds"`
	Topics  []NamedSecs `json:"topics"`
}

type AppStat struct {
	Name        string `json:"name"`
	Exe         string `json:"exe"`
	Seconds     int64  `json:"seconds"`
	Productive  int64  `json:"productive"`
	Distraction int64  `json:"distraction"`
}

type SiteStat struct {
	Site    string `json:"site"`
	Seconds int64  `json:"seconds"`
	Kind    string `json:"kind"`

	prod, dist int64
}

type ActivityStat struct {
	Label    string `json:"label"`
	App      string `json:"app"`
	Site     string `json:"site"`
	Category string `json:"category"`
	Subject  string `json:"subject"`
	Topic    string `json:"topic"`
	Kind     string `json:"kind"`
	Seconds  int64  `json:"seconds"`
}

type TimelineBlock struct {
	Start    int64  `json:"start"`
	End      int64  `json:"end"`
	Kind     string `json:"kind"`
	Category string `json:"category"`
	Label    string `json:"label"`
	App      string `json:"app"`

	key string
}

type DeepWorkBlock struct {
	Start    int64  `json:"start"`
	End      int64  `json:"end"`
	Seconds  int64  `json:"seconds"`
	Category string `json:"category"`
	Label    string `json:"label"`
}

type DayReport struct {
	Total       int64           `json:"total"`
	Productive  int64           `json:"productive"`
	Distraction int64           `json:"distraction"`
	Other       int64           `json:"other"`
	FocusScore  *int            `json:"focusScore"`
	ByCategory  []CategoryStat  `json:"byCategory"`
	BySubject   []SubjectStat   `json:"bySubject"`
	ByApp       []AppStat       `json:"byApp"`
	BySite      []SiteStat      `json:"bySite"`
	Activities  []ActivityStat  `json:"activities"`
	Timeline    []TimelineBlock `json:"timeline"`
	DeepWork    []DeepWorkBlock `json:"deepWork"`
	FirstTs     int64           `json:"firstTs"`
	LastTs      int64           `json:"lastTs"`
}

type WeekDay struct {
	Date        string `json:"date"`
	Label       string `json:"label"`
	Productive  int64  `json:"productive"`
	Distraction int64  `json:"distraction"`
	Other       int64  `json:"other"`
}

const (
	deepWorkGapSecs   = 5 * 60  // productive sessions this close merge
	deepWorkBreakSecs = 60      // a non-productive stretch this long breaks a block
	deepWorkMinSecs   = 45 * 60 // minimum deep work block
	timelineGapSecs   = 2 * 60
	topApps           = 12
	topSites          = 12
	topActivities     = 30
	topTopics         = 6
)

// dayBounds returns local midnight..next midnight for d.
func dayBounds(d time.Time) (int64, int64) {
	start := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, d.Location())
	return start.Unix(), start.AddDate(0, 0, 1).Unix()
}

type daySeg struct {
	start, end int64
	r          resolved
}

// Day builds the full report for one local day.
func (a *Analyzer) Day(d time.Time) (*DayReport, error) {
	from, to := dayBounds(d)
	rows, err := a.store.SessionsBetween(from, to)
	if err != nil {
		return nil, err
	}

	rep := &DayReport{
		ByCategory: []CategoryStat{},
		BySubject:  []SubjectStat{},
		ByApp:      []AppStat{},
		BySite:     []SiteStat{},
		Activities: []ActivityStat{},
		Timeline:   []TimelineBlock{},
		DeepWork:   []DeepWorkBlock{},
	}
	cats := map[string]*CategoryStat{}
	subjects := map[string]*SubjectStat{}
	topics := map[string]map[string]int64{}
	apps := map[string]*AppStat{}
	sites := map[string]*SiteStat{}
	acts := map[string]*ActivityStat{}
	var segs []daySeg

	for _, s := range rows {
		start, end := max(s.Start, from), min(s.End, to)
		if end <= start {
			continue
		}
		secs := end - start
		r := a.resolve(s)
		segs = append(segs, daySeg{start, end, r})

		switch r.Kind {
		case kindProductive:
			rep.Productive += secs
		case kindDistraction:
			rep.Distraction += secs
		default:
			rep.Other += secs
		}

		c := cats[r.Category]
		if c == nil {
			c = &CategoryStat{Name: r.Category, Kind: r.Kind}
			cats[r.Category] = c
		}
		c.Seconds += secs

		if r.Kind == kindProductive && r.Subject != "" {
			sj := subjects[r.Subject]
			if sj == nil {
				sj = &SubjectStat{Name: r.Subject}
				subjects[r.Subject] = sj
				topics[r.Subject] = map[string]int64{}
			}
			sj.Seconds += secs
			topic := r.Topic
			if topic == "" {
				topic = r.Label
			}
			topics[r.Subject][topic] += secs
		}

		ap := apps[s.App]
		if ap == nil {
			ap = &AppStat{Name: r.App, Exe: s.App}
			apps[s.App] = ap
		}
		ap.Seconds += secs
		switch r.Kind {
		case kindProductive:
			ap.Productive += secs
		case kindDistraction:
			ap.Distraction += secs
		}

		if r.Site != "" {
			st := sites[r.Site]
			if st == nil {
				st = &SiteStat{Site: r.Site}
				sites[r.Site] = st
			}
			st.Seconds += secs
			switch r.Kind {
			case kindProductive:
				st.prod += secs
			case kindDistraction:
				st.dist += secs
			}
		}

		ac := acts[r.Key]
		if ac == nil {
			ac = &ActivityStat{
				Label: r.Label, App: r.App, Site: r.Site, Category: r.Category,
				Subject: r.Subject, Topic: r.Topic, Kind: r.Kind,
			}
			acts[r.Key] = ac
		}
		ac.Seconds += secs

		if rep.FirstTs == 0 || start < rep.FirstTs {
			rep.FirstTs = start
		}
		if end > rep.LastTs {
			rep.LastTs = end
		}
	}

	rep.Total = rep.Productive + rep.Distraction + rep.Other
	if rep.Total > 0 {
		score := int(math.Round(float64(rep.Productive) * 100 / float64(rep.Total)))
		rep.FocusScore = &score
	}

	for _, c := range cats {
		rep.ByCategory = append(rep.ByCategory, *c)
	}
	sort.Slice(rep.ByCategory, func(i, j int) bool { return rep.ByCategory[i].Seconds > rep.ByCategory[j].Seconds })

	for name, sj := range subjects {
		for t, secs := range topics[name] {
			sj.Topics = append(sj.Topics, NamedSecs{Name: t, Seconds: secs})
		}
		sort.Slice(sj.Topics, func(i, j int) bool { return sj.Topics[i].Seconds > sj.Topics[j].Seconds })
		if len(sj.Topics) > topTopics {
			sj.Topics = sj.Topics[:topTopics]
		}
		rep.BySubject = append(rep.BySubject, *sj)
	}
	sort.Slice(rep.BySubject, func(i, j int) bool { return rep.BySubject[i].Seconds > rep.BySubject[j].Seconds })

	for _, ap := range apps {
		rep.ByApp = append(rep.ByApp, *ap)
	}
	sort.Slice(rep.ByApp, func(i, j int) bool { return rep.ByApp[i].Seconds > rep.ByApp[j].Seconds })
	if len(rep.ByApp) > topApps {
		rep.ByApp = rep.ByApp[:topApps]
	}

	for _, st := range sites {
		switch {
		case st.prod > 0 && st.prod >= st.dist:
			st.Kind = kindProductive
		case st.dist > 0:
			st.Kind = kindDistraction
		default:
			st.Kind = kindNeutral
		}
		rep.BySite = append(rep.BySite, *st)
	}
	sort.Slice(rep.BySite, func(i, j int) bool { return rep.BySite[i].Seconds > rep.BySite[j].Seconds })
	if len(rep.BySite) > topSites {
		rep.BySite = rep.BySite[:topSites]
	}

	for _, ac := range acts {
		rep.Activities = append(rep.Activities, *ac)
	}
	sort.Slice(rep.Activities, func(i, j int) bool { return rep.Activities[i].Seconds > rep.Activities[j].Seconds })
	if len(rep.Activities) > topActivities {
		rep.Activities = rep.Activities[:topActivities]
	}

	sort.Slice(segs, func(i, j int) bool { return segs[i].start < segs[j].start })
	for _, sg := range segs {
		if n := len(rep.Timeline); n > 0 {
			last := &rep.Timeline[n-1]
			if last.key == sg.r.Key && sg.start-last.End <= timelineGapSecs {
				last.End = max(last.End, sg.end)
				continue
			}
		}
		rep.Timeline = append(rep.Timeline, TimelineBlock{
			Start: sg.start, End: sg.end, Kind: sg.r.Kind, Category: sg.r.Category,
			Label: sg.r.Label, App: sg.r.App, key: sg.r.Key,
		})
	}
	kept := rep.Timeline[:0]
	for _, b := range rep.Timeline {
		if b.End-b.Start >= 30 {
			kept = append(kept, b)
		}
	}
	rep.Timeline = kept

	rep.DeepWork = deepWorkBlocks(segs)
	return rep, nil
}

// deepWorkBlocks merges productive segments separated by gaps of
// at most 5 minutes (short non-productive blips don't count as a
// break) and keeps blocks of 45+ minutes. segs must be sorted.
func deepWorkBlocks(segs []daySeg) []DeepWorkBlock {
	out := []DeepWorkBlock{}
	type block struct {
		start, end int64
		cats       map[string]int64
		labels     map[string]int64
	}
	var cur *block

	top := func(m map[string]int64) string {
		best, bestSecs := "", int64(-1)
		for k, v := range m {
			if v > bestSecs || (v == bestSecs && k < best) {
				best, bestSecs = k, v
			}
		}
		return best
	}
	flush := func() {
		if cur != nil && cur.end-cur.start >= deepWorkMinSecs {
			out = append(out, DeepWorkBlock{
				Start: cur.start, End: cur.end, Seconds: cur.end - cur.start,
				Category: top(cur.cats), Label: top(cur.labels),
			})
		}
		cur = nil
	}

	for _, sg := range segs {
		secs := sg.end - sg.start
		if sg.r.Kind != kindProductive {
			if secs >= deepWorkBreakSecs {
				flush()
			}
			continue
		}
		if cur == nil || sg.start-cur.end > deepWorkGapSecs {
			flush()
			cur = &block{start: sg.start, end: sg.end, cats: map[string]int64{}, labels: map[string]int64{}}
		}
		cur.end = max(cur.end, sg.end)
		cur.cats[sg.r.Category] += secs
		cur.labels[sg.r.Label] += secs
	}
	flush()
	return out
}

// Week returns productive/distraction/other totals for the 7 days
// ending on end (inclusive), oldest first.
func (a *Analyzer) Week(end time.Time) ([]WeekDay, error) {
	days := make([]WeekDay, 7)
	bounds := make([]int64, 8)
	for i := 0; i < 7; i++ {
		d := end.AddDate(0, 0, i-6)
		bounds[i], _ = dayBounds(d)
		days[i] = WeekDay{Date: d.Format("2006-01-02"), Label: d.Format("Mon")}
	}
	_, bounds[7] = dayBounds(end)

	rows, err := a.store.SessionsBetween(bounds[0], bounds[7])
	if err != nil {
		return nil, err
	}
	for _, s := range rows {
		r := a.resolve(s)
		for i := 0; i < 7; i++ {
			secs := min(s.End, bounds[i+1]) - max(s.Start, bounds[i])
			if secs <= 0 {
				continue
			}
			switch r.Kind {
			case kindProductive:
				days[i].Productive += secs
			case kindDistraction:
				days[i].Distraction += secs
			default:
				days[i].Other += secs
			}
		}
	}
	return days, nil
}

// dailyReport is the text of the 9 PM toast.
func dailyReport(a *Analyzer) (string, error) {
	rep, err := a.Day(time.Now())
	if err != nil {
		return "", err
	}
	msg := fmt.Sprintf("Today: %s productive, %s distraction, %s other",
		fmtDur(rep.Productive), fmtDur(rep.Distraction), fmtDur(rep.Other))
	if len(rep.BySubject) > 0 {
		var parts []string
		for i, s := range rep.BySubject {
			if i == 3 {
				break
			}
			parts = append(parts, s.Name+" "+fmtDur(s.Seconds))
		}
		msg += ". Top: " + strings.Join(parts, ", ")
	}
	return msg, nil
}
