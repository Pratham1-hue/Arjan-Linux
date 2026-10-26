package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The classifier sends each distinct activity (app name, website
// domain, window/tab title — never full URLs) to an OpenAI-
// compatible chat endpoint once, and caches the answer forever in
// the ai_labels table. Opt-in; off by default.

const (
	aiInterval     = 5 * time.Minute
	aiBatchSize    = 25
	aiMaxBatches   = 8 // per run: at most 200 activities
	aiMinSecs      = 60
	aiLookbackDays = 7
	aiMaxAttempts  = 3
)

// System UI with nothing worth classifying.
var aiSkipApps = map[string]bool{
	// Windows shell / system
	"explorer.exe":                true,
	"searchhost.exe":              true,
	"searchapp.exe":               true,
	"shellexperiencehost.exe":     true,
	"startmenuexperiencehost.exe": true,
	"lockapp.exe":                 true,
	"taskmgr.exe":                 true,
	"systemsettings.exe":          true,
	"textinputhost.exe":           true,
	"tracker.exe":                 true,
	// Linux shell / system / lock screens
	"gnome-shell":       true,
	"plasmashell":       true,
	"kwin_x11":          true,
	"kwin_wayland":      true,
	"gnome-screensaver": true,
	"xscreensaver":      true,
	"light-locker":      true,
	"i3lock":            true,
	"swaylock":          true,
	"arjan":             true,
	"tracker":           true,
	"unknown":           true,
}

const aiSystemPrompt = `You label computer activity for a student's personal study tracker.
Each input item has an id, the app name, the website domain (if any) and the window or tab title.
For every item return an object with:
- "id": the same id
- "kind": "productive" (studying, lectures, tutorials, documentation, courses, coding, research, AI assistants used for study or work), "distraction" (entertainment videos, short-form video, social media feeds, memes, games, shopping) or "neutral" (music, system tools, file browsing, settings, new tab page, messaging)
- "category": a short activity label such as "Lecture", "Coding", "Documentation", "AI Assistant", "Practice Problems", "Reading", "Entertainment", "Social Media", "Music", "Communication", "System"
- "subject": the academic or technical subject being learned or worked on, in Title Case, e.g. "Machine Learning", "Deep Learning", "Statistics", "Linear Algebra", "Data Structures & Algorithms", "Web Development", "Python", "Databases"; "" if not studying or unclear
- "topic": the specific topic in at most 6 words, e.g. "Gradient Descent", "Binary Search Trees", "React Hooks"; "" if unclear
Judge by the title: a YouTube lecture is productive, a YouTube vlog or meme is a distraction.
Use the same subject names consistently across items.
Respond with JSON only, in the form {"items":[{"id":1,"kind":"...","category":"...","subject":"...","topic":"..."}]}`

type AIStatus struct {
	Enabled    bool   `json:"enabled"`
	Configured bool   `json:"configured"`
	Running    bool   `json:"running"`
	LastRun    int64  `json:"lastRun"`
	LastError  string `json:"lastError"`
	Labeled    int    `json:"labeled"`
	Pending    int    `json:"pending"`
}

type pendingItem struct {
	Key, App, Site, Title string
	Secs                  int64
}

type Classifier struct {
	store   *Store
	rules   *RuleEngine
	labels  *LabelCache
	cfg     *ConfigStore
	lg      *log.Logger
	client  *http.Client
	trigger chan struct{}

	mu       sync.Mutex
	status   AIStatus
	attempts map[string]int
	// backoffUntil pauses classification until a provider quota
	// resets (e.g. Gemini's daily free-tier limit), instead of
	// hammering it with doomed requests for hours.
	backoffUntil time.Time
}

func NewClassifier(store *Store, rules *RuleEngine, labels *LabelCache, cfg *ConfigStore, lg *log.Logger) *Classifier {
	return &Classifier{
		store: store, rules: rules, labels: labels, cfg: cfg, lg: lg,
		client:   &http.Client{Timeout: 90 * time.Second},
		trigger:  make(chan struct{}, 1),
		attempts: map[string]int{},
	}
}

func (c *Classifier) Status() AIStatus {
	c.mu.Lock()
	st := c.status
	c.mu.Unlock()
	cfg := c.cfg.Get().AI
	st.Enabled = cfg.Enabled
	st.Configured = cfg.BaseURL != "" && cfg.Model != "" &&
		(c.cfg.APIKey() != "" || isLocalURL(cfg.BaseURL))
	st.Labeled = c.labels.Len()
	return st
}

// Trigger asks the worker to run now (non-blocking).
func (c *Classifier) Trigger() {
	select {
	case c.trigger <- struct{}{}:
	default:
	}
}

// Run classifies new activities every 5 minutes until ctx ends.
func (c *Classifier) Run(ctx context.Context) {
	first := time.NewTimer(20 * time.Second)
	defer first.Stop()
	tick := time.NewTicker(aiInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-first.C:
		case <-tick.C:
		case <-c.trigger:
		}
		c.runOnce(ctx)
	}
}

func (c *Classifier) setStatus(fn func(*AIStatus)) {
	c.mu.Lock()
	fn(&c.status)
	c.mu.Unlock()
}

// setBackoffUntil pauses (or, with the zero time, resumes) classification.
func (c *Classifier) setBackoffUntil(t time.Time) {
	c.mu.Lock()
	c.backoffUntil = t
	c.mu.Unlock()
}

// ResetBackoff resumes classifying immediately (called when the user
// changes the AI settings — e.g. switches to another provider).
func (c *Classifier) ResetBackoff() { c.setBackoffUntil(time.Time{}) }

// backingOff reports how long classification is still paused.
func (c *Classifier) backingOff() (time.Duration, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d := time.Until(c.backoffUntil); d > 0 {
		return d, true
	}
	return 0, false
}

func (c *Classifier) runOnce(ctx context.Context) {
	if _, off := c.backingOff(); off {
		return // waiting out a quota reset; status already has the error
	}
	pending, err := c.pending()
	if err != nil {
		c.setStatus(func(s *AIStatus) { s.LastError = err.Error() })
		return
	}
	c.setStatus(func(s *AIStatus) { s.Pending = len(pending) })

	cfg := c.cfg.Get().AI
	if !cfg.Enabled || len(pending) == 0 {
		return
	}
	key := c.cfg.APIKey()
	if key == "" && !isLocalURL(cfg.BaseURL) {
		c.setStatus(func(s *AIStatus) { s.LastError = "No API key — add one in Settings" })
		return
	}

	c.setStatus(func(s *AIStatus) { s.Running = true })
	labeled := 0
	for b := 0; b < aiMaxBatches && len(pending) > 0 && ctx.Err() == nil; b++ {
		n := min(aiBatchSize, len(pending))
		batch := pending[:n]
		pending = pending[n:]

		got, err := c.classify(ctx, cfg, key, batch)
		if err != nil {
			c.setStatus(func(s *AIStatus) { s.LastError = err.Error() })
			c.lg.Printf("AI classify: %v", err)
			if d, ok := quotaReset(err); ok {
				// Daily quota exhausted: stop trying until it resets.
				c.setBackoffUntil(time.Now().Add(d))
				c.lg.Printf("AI paused for %s (quota reset)", fmtDur(int64(d.Seconds())))
			}
			break
		}
		c.setBackoffUntil(time.Time{}) // working again
		now := time.Now().Unix()
		for i, it := range batch {
			l, ok := got[i]
			if !ok {
				c.mu.Lock()
				c.attempts[it.Key]++
				c.mu.Unlock()
				continue
			}
			if err := c.store.SaveLabel(it.Key, l, cfg.Model, now); err != nil {
				c.lg.Printf("AI save label: %v", err)
				continue
			}
			c.labels.Set(it.Key, l)
			labeled++
		}
		c.setStatus(func(s *AIStatus) { s.LastError = "" })
	}

	remaining, _ := c.pending()
	c.setStatus(func(s *AIStatus) {
		s.Running = false
		s.LastRun = time.Now().Unix()
		s.Pending = len(remaining)
	})
	if labeled > 0 {
		c.lg.Printf("AI: labeled %d activities", labeled)
	}
}

// pending lists unlabeled activities from the last week worth
// classifying, longest first.
func (c *Classifier) pending() ([]pendingItem, error) {
	since := time.Now().AddDate(0, 0, -aiLookbackDays).Unix()
	totals, err := c.store.ActivityTotals(since)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	attempts := make(map[string]int, len(c.attempts))
	for k, v := range c.attempts {
		attempts[k] = v
	}
	c.mu.Unlock()

	agg := map[string]*pendingItem{}
	for _, t := range totals {
		if aiSkipApps[strings.ToLower(t.App)] {
			continue
		}
		// rules.json already settles distractions and neutral
		// categories; productive ones still get a subject/topic.
		if cat := c.rules.Categorize(t.App, t.Title, t.URL); cat != uncategorized &&
			c.rules.KindOf(cat) != kindProductive {
			continue
		}
		key := activityKey(t.App, t.URL, t.Title)
		if _, done := c.labels.Get(key); done || attempts[key] >= aiMaxAttempts {
			continue
		}
		p := agg[key]
		if p == nil {
			p = &pendingItem{Key: key, App: t.App, Site: siteOf(t.URL), Title: normTitle(t.Title)}
			agg[key] = p
		}
		p.Secs += t.Secs
	}

	out := make([]pendingItem, 0, len(agg))
	for _, p := range agg {
		if p.Secs >= aiMinSecs {
			out = append(out, *p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Secs > out[j].Secs })
	return out, nil
}

// classify labels a batch. The result is keyed by index into items;
// items the model skipped are simply missing.
func (c *Classifier) classify(ctx context.Context, cfg AIConfig, key string, items []pendingItem) (map[int]labelInfo, error) {
	type input struct {
		ID    int    `json:"id"`
		App   string `json:"app"`
		Site  string `json:"site,omitempty"`
		Title string `json:"title"`
	}
	list := make([]input, len(items))
	for i, it := range items {
		list[i] = input{ID: i + 1, App: appName(it.App), Site: it.Site, Title: clip(it.Title, 200)}
	}
	payload, _ := json.Marshal(list)

	content, err := chatComplete(ctx, c.client, cfg, key, aiSystemPrompt, "Classify these activities:\n"+string(payload))
	if err != nil {
		return nil, err
	}
	parsed, err := parseLabels(content)
	if err != nil {
		return nil, err
	}
	out := map[int]labelInfo{}
	for _, p := range parsed {
		if idx := int(p.ID) - 1; idx >= 0 && idx < len(items) {
			out[idx] = p.label()
		}
	}
	return out, nil
}

// Test classifies two fixed samples with the saved settings.
func (c *Classifier) Test(ctx context.Context) ([]map[string]string, error) {
	cfg := c.cfg.Get().AI
	if cfg.BaseURL == "" || cfg.Model == "" {
		return nil, errors.New("set a base URL and model first")
	}
	key := c.cfg.APIKey()
	if key == "" && !isLocalURL(cfg.BaseURL) {
		return nil, errors.New("no API key saved")
	}
	samples := []pendingItem{
		{App: "chrome.exe", Site: "youtube.com", Title: "Gradient Descent, Step-by-Step - StatQuest - YouTube"},
		{App: "chrome.exe", Site: "instagram.com", Title: "Instagram"},
	}
	got, err := c.classify(ctx, cfg, key, samples)
	if err != nil {
		return nil, err
	}
	var out []map[string]string
	for i, s := range samples {
		l := got[i]
		out = append(out, map[string]string{
			"title": s.Title, "kind": l.Kind, "category": l.Category,
			"subject": l.Subject, "topic": l.Topic,
		})
	}
	return out, nil
}

// Clear forgets every AI label so everything is classified again.
func (c *Classifier) Clear() error {
	if err := c.store.ClearLabels(); err != nil {
		return err
	}
	c.labels.Replace(map[string]labelInfo{})
	c.mu.Lock()
	c.attempts = map[string]int{}
	c.mu.Unlock()
	c.Trigger()
	return nil
}

// Models lists model ids offered by the configured endpoint.
func (c *Classifier) Models(ctx context.Context) ([]string, error) {
	cfg := c.cfg.Get().AI
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(cfg.BaseURL, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	if key := c.cfg.APIKey(); anthropicStyle(cfg) {
		// Anthropic wants its own auth headers, not a Bearer token.
		if key != "" {
			req.Header.Set("x-api-key", key)
		}
		req.Header.Set("anthropic-version", anthropicVersion)
	} else if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("%s: %s", resp.Status, apiErrorMessage(raw))
	}
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("unexpected response: %w", err)
	}
	ids := make([]string, 0, len(list.Data))
	for _, m := range list.Data {
		id := strings.TrimPrefix(m.ID, "models/")
		if !isNonChatModel(id) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// nonChatMarkers identify models that can't do text chat
// (speech, image/video/music generation, embeddings, ...).
var nonChatMarkers = []string{
	"tts", "embed", "imagen", "image", "veo", "lyria", "aqa",
	"native-audio", "transcribe", "live", "robotics", "computer-use", "whisper", "dall-e",
}

func isNonChatModel(id string) bool {
	id = strings.ToLower(id)
	for _, m := range nonChatMarkers {
		if strings.Contains(id, m) {
			return true
		}
	}
	return false
}

// looksLikeAPIKey rejects obviously wrong pastes (an error message,
// a sentence, a URL...). Real keys are a single token.
func looksLikeAPIKey(k string) bool {
	if len(k) < 8 || len(k) > 256 {
		return false
	}
	for _, r := range k {
		if r <= ' ' || r > '~' {
			return false
		}
	}
	return true
}

// ------------------------------------------------------------ HTTP client ---

// anthropicStyle selects Anthropic's Messages API (which is not
// OpenAI-compatible) instead of the chat-completions protocol.
// Detected from the preset or the URL, so a custom proxy works too.
func anthropicStyle(cfg AIConfig) bool {
	return cfg.Provider == "anthropic" || isAnthropicURL(cfg.BaseURL)
}

func isAnthropicURL(raw string) bool {
	return strings.Contains(raw, "api.anthropic.com")
}

// anthropicVersion is the required version header for /v1/messages.
const anthropicVersion = "2023-06-01"

// quotaResetRE finds wait hints like "retry in 18h11m44.841786254s"
// that providers embed in quota error messages.
var quotaResetRE = regexp.MustCompile(`(?i)retry in (?:(\d+)h)?(?:([\d.]+)m)?(?:([\d.]+)s)?`)

// quotaReset reports how long a provider says to wait before the
// next attempt (false when the message has no usable hint).
func quotaReset(err error) (time.Duration, bool) {
	if err == nil {
		return 0, false
	}
	m := quotaResetRE.FindStringSubmatch(err.Error())
	if m == nil {
		return 0, false
	}
	var d time.Duration
	for i, unit := range []time.Duration{time.Hour, time.Minute, time.Second} {
		if m[i+1] == "" {
			continue
		}
		f, convErr := strconv.ParseFloat(m[i+1], 64)
		if convErr != nil {
			return 0, false
		}
		d += time.Duration(f * float64(unit))
	}
	if d < time.Second {
		return 0, false
	}
	return d, true
}

func chatComplete(ctx context.Context, client *http.Client, cfg AIConfig, key, system, user string) (string, error) {
	var (
		content string
		status  int
		err     error
	)

	if anthropicStyle(cfg) {
		content, status, err = retrying(ctx, func() (string, int, time.Duration, error) {
			return anthropicChat(ctx, client, cfg, key, system, user)
		})
	} else {
		body := map[string]any{
			"model": cfg.Model,
			"messages": []map[string]string{
				{"role": "system", "content": system},
				{"role": "user", "content": user},
			},
			"response_format": map[string]string{"type": "json_object"},
		}
		if isGeminiURL(cfg.BaseURL) {
			body["reasoning_effort"] = "low" // fast and cheap; this is easy work
		}
		content, status, err = postChatRetry(ctx, client, cfg.BaseURL, key, body)
		if err != nil && status == http.StatusBadRequest {
			// Some providers reject optional fields; retry plain.
			delete(body, "response_format")
			delete(body, "reasoning_effort")
			content, status, err = postChatRetry(ctx, client, cfg.BaseURL, key, body)
		}
	}

	if err != nil {
		if d, ok := quotaReset(err); ok && (status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable) {
			err = fmt.Errorf("%w — free-tier daily quota reached; Arjan pauses AI for ~%s, then tries again "+
				"(switch provider in Settings if you don't want to wait)", err, fmtDur(int64(d.Seconds())))
		} else {
			switch status {
			case http.StatusServiceUnavailable, http.StatusTooManyRequests:
				err = fmt.Errorf("%w — the provider is busy or rate-limiting; Arjan will retry automatically "+
					"(or pick a lighter model via Settings → Load models)", err)
			}
		}
	}
	return content, err
}

// anthropicChat performs one POST /v1/messages call and returns the
// text of the reply.
func anthropicChat(ctx context.Context, client *http.Client, cfg AIConfig, key, system, user string) (string, int, time.Duration, error) {
	body := map[string]any{
		"model":      cfg.Model,
		"max_tokens": 4096,
		"system":     system,
		"messages":   []map[string]string{{"role": "user", "content": user}},
	}
	data, err := json.Marshal(body)
	if err != nil {
		return "", 0, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(cfg.BaseURL, "/")+"/messages", bytes.NewReader(data))
	if err != nil {
		return "", 0, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", anthropicVersion)

	resp, err := client.Do(req)
	if err != nil {
		return "", 0, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))

	if resp.StatusCode/100 != 2 {
		e := fmt.Errorf("%s: %s", resp.Status, apiErrorMessage(raw))
		if secs, perr := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After"))); perr == nil && secs > 0 {
			return "", resp.StatusCode, time.Duration(secs) * time.Second, e
		}
		return "", resp.StatusCode, 0, e
	}

	var cr struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &cr); err != nil {
		return "", resp.StatusCode, 0, fmt.Errorf("unexpected response: %w", err)
	}
	var out strings.Builder
	for _, blk := range cr.Content {
		if blk.Type == "text" || blk.Type == "" {
			out.WriteString(blk.Text)
		}
	}
	if out.Len() == 0 {
		return "", resp.StatusCode, 0, errors.New("empty response from model")
	}
	return out.String(), resp.StatusCode, 0, nil
}

// retryDelays are the waits between attempts on temporary failures
// (overloaded, rate limited, gateway errors, network hiccups).
var retryDelays = []time.Duration{2 * time.Second, 5 * time.Second, 12 * time.Second}

func isTransient(status int, err error) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	case 0: // no HTTP response: network error / timeout
		return err != nil && !errors.Is(err, context.Canceled)
	}
	return false
}

// retrying runs fn, retrying temporary failures with growing waits
// (shared by the OpenAI-style and Anthropic paths).
func retrying(ctx context.Context, fn func() (string, int, time.Duration, error)) (string, int, error) {
	for attempt := 0; ; attempt++ {
		content, status, retryAfter, err := fn()
		if err == nil || attempt >= len(retryDelays) || !isTransient(status, err) {
			return content, status, err
		}
		if d, ok := quotaReset(err); ok && d > time.Minute {
			return content, status, err // reset is far away; retrying now only wastes attempts
		}
		wait := retryDelays[attempt]
		if retryAfter > 0 && retryAfter <= time.Minute {
			wait = retryAfter // honor the provider's Retry-After
		}
		select {
		case <-ctx.Done():
			return "", status, ctx.Err()
		case <-time.After(wait):
		}
	}
}

func postChatRetry(ctx context.Context, client *http.Client, baseURL, key string, body map[string]any) (string, int, error) {
	return retrying(ctx, func() (string, int, time.Duration, error) {
		return postChat(ctx, client, baseURL, key, body)
	})
}

func postChat(ctx context.Context, client *http.Client, baseURL, key string, body map[string]any) (string, int, time.Duration, error) {
	content, status, err := doPostChat(ctx, client, baseURL, key, body)
	return content, status, retryAfterOf(err), stripRetryAfter(err)
}

// retryAfterErr carries a Retry-After hint out of doPostChat.
type retryAfterErr struct {
	err   error
	after time.Duration
}

func (e *retryAfterErr) Error() string { return e.err.Error() }
func (e *retryAfterErr) Unwrap() error { return e.err }

func retryAfterOf(err error) time.Duration {
	var ra *retryAfterErr
	if errors.As(err, &ra) {
		return ra.after
	}
	return 0
}

func stripRetryAfter(err error) error {
	var ra *retryAfterErr
	if errors.As(err, &ra) {
		return ra.err
	}
	return err
}

func doPostChat(ctx context.Context, client *http.Client, baseURL, key string, body map[string]any) (string, int, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return "", 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode/100 != 2 {
		err := fmt.Errorf("%s: %s", resp.Status, apiErrorMessage(raw))
		if secs, perr := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After"))); perr == nil && secs > 0 {
			return "", resp.StatusCode, &retryAfterErr{err: err, after: time.Duration(secs) * time.Second}
		}
		return "", resp.StatusCode, err
	}
	var cr struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &cr); err != nil {
		return "", resp.StatusCode, fmt.Errorf("unexpected response: %w", err)
	}
	if len(cr.Choices) == 0 {
		return "", resp.StatusCode, errors.New("empty response from model")
	}
	return cr.Choices[0].Message.Content, resp.StatusCode, nil
}

func apiErrorMessage(raw []byte) string {
	var obj struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &obj) == nil && obj.Error.Message != "" {
		return obj.Error.Message
	}
	var arr []struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &arr) == nil && len(arr) > 0 && arr[0].Error.Message != "" {
		return arr[0].Error.Message
	}
	s := strings.TrimSpace(string(raw))
	return clip(s, 300)
}

func isLocalURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

func isGeminiURL(raw string) bool {
	return strings.Contains(raw, "generativelanguage.googleapis.com")
}

// ----------------------------------------------------------- parsing ---

// flexInt accepts 1 or "1" (some models quote ids).
type flexInt int

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return err
	}
	*f = flexInt(n)
	return nil
}

type aiItem struct {
	ID       flexInt `json:"id"`
	Kind     string  `json:"kind"`
	Category string  `json:"category"`
	Subject  string  `json:"subject"`
	Topic    string  `json:"topic"`
}

func (it aiItem) label() labelInfo {
	return labelInfo{
		Kind:     normalizeKind(it.Kind),
		Category: cleanField(it.Category, 40),
		Subject:  cleanField(it.Subject, 40),
		Topic:    cleanField(it.Topic, 60),
	}
}

// parseLabels tolerates code fences, prose around the JSON, and a
// bare array instead of {"items": [...]}.
func parseLabels(content string) ([]aiItem, error) {
	s := strings.TrimSpace(content)
	if i, j := strings.Index(s, "{"), strings.LastIndex(s, "}"); i >= 0 && j > i {
		var obj struct {
			Items []aiItem `json:"items"`
		}
		if err := json.Unmarshal([]byte(s[i:j+1]), &obj); err == nil && len(obj.Items) > 0 {
			return obj.Items, nil
		}
	}
	if i, j := strings.Index(s, "["), strings.LastIndex(s, "]"); i >= 0 && j > i {
		var arr []aiItem
		if err := json.Unmarshal([]byte(s[i:j+1]), &arr); err == nil {
			return arr, nil
		}
	}
	return nil, fmt.Errorf("could not parse AI response: %s", clip(s, 120))
}

func normalizeKind(k string) string {
	k = strings.ToLower(strings.TrimSpace(k))
	switch {
	case strings.Contains(k, "distract"):
		return kindDistraction
	case strings.Contains(k, "product"), strings.Contains(k, "learn"),
		strings.Contains(k, "study"), strings.Contains(k, "work"):
		return kindProductive
	}
	return kindNeutral
}

func cleanField(s string, n int) string {
	s = strings.TrimSpace(s)
	switch strings.ToLower(s) {
	case "none", "n/a", "na", "-", "null", "unknown":
		return ""
	}
	return clip(s, n)
}
