package main

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"
)

// Activity kinds drive the daily report and the distraction nudge.
const (
	kindProductive  = "productive"
	kindDistraction = "distraction"
	kindNeutral     = "neutral" // Music, Uncategorized, ... counts as "other"

	uncategorized = "Uncategorized"
)

// rule is one entry of rules.json. Each match string is a
// case-insensitive substring tested against the URL, the window
// title and the executable name. The first category that matches
// wins, so order matters: put specific rules before general ones.
type rule struct {
	Name  string   `json:"name"`
	Kind  string   `json:"kind"` // productive | distraction | (anything else = neutral)
	Match []string `json:"match"`
}

type rulesFile struct {
	Categories []rule `json:"categories"`
}

// RuleEngine loads rules.json and maps activities to categories.
// It is safe for concurrent use, and Reload() hot-reloads the
// file when it changes on disk.
type RuleEngine struct {
	path string
	mod  time.Time

	mu   sync.RWMutex
	cats []rule
}

func NewRuleEngine(path string) *RuleEngine {
	return &RuleEngine{path: path}
}

// Path returns the file the engine reads.
func (e *RuleEngine) Path() string { return e.path }

// Reload re-reads rules.json if it was modified since the last
// successful load. A file that fails to parse is ignored (the
// old rules stay in force), so a half-saved edit can't break
// the daemon.
func (e *RuleEngine) Reload() error {
	st, err := os.Stat(e.path)
	if err != nil {
		return err
	}
	if !st.ModTime().After(e.mod) {
		return nil // unchanged
	}
	data, err := os.ReadFile(e.path)
	if err != nil {
		return err
	}
	var rf rulesFile
	if err := json.Unmarshal(data, &rf); err != nil {
		return err
	}
	e.mu.Lock()
	e.cats = rf.Categories
	e.mu.Unlock()
	e.mod = st.ModTime()
	return nil
}

// Categorize returns the category for an activity. The friendly app
// name is tested too, so one rule ("VS Code") matches both
// "code.exe" on Windows and "code" on Linux.
func (e *RuleEngine) Categorize(app, title, url string) string {
	e.mu.RLock()
	cats := e.cats
	e.mu.RUnlock()

	haystack := strings.ToLower(url + "\n" + title + "\n" + app + "\n" + appName(app))
	for _, c := range cats {
		for _, m := range c.Match {
			if m != "" && strings.Contains(haystack, strings.ToLower(m)) {
				return c.Name
			}
		}
	}
	return uncategorized
}

// KindOf reports the kind of a category name. Unknown categories
// are neutral.
func (e *RuleEngine) KindOf(category string) string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, c := range e.cats {
		if strings.EqualFold(c.Name, category) {
			switch k := strings.ToLower(c.Kind); k {
			case kindProductive, kindDistraction:
				return k
			}
			return kindNeutral
		}
	}
	return kindNeutral
}
