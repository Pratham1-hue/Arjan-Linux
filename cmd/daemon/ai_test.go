package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeLLM is a minimal OpenAI-compatible endpoint. Like some real
// providers, it rejects response_format, so the client's plain
// retry path is exercised too.
func fakeLLM(t *testing.T, calls *atomic.Int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, `{"error":{"message":"bad key"}}`, http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v1/models":
			io.WriteString(w, `{"data":[{"id":"models/b-model"},{"id":"a-model"}]}`)
		case "/v1/chat/completions":
			calls.Add(1)
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			json.Unmarshal(raw, &body)
			if _, ok := body["response_format"]; ok {
				http.Error(w, `{"error":{"message":"response_format unsupported"}}`, http.StatusBadRequest)
				return
			}
			msgs := body["messages"].([]any)
			user := msgs[1].(map[string]any)["content"].(string)
			var items []string
			if strings.Contains(user, "Gradient") {
				items = append(items, `{"id":1,"kind":"productive","category":"Lecture","subject":"Machine Learning","topic":"Gradient Descent"}`)
			}
			if strings.Contains(user, "Instagram") {
				items = append(items, `{"id":2,"kind":"distraction","category":"Social Media","subject":"","topic":""}`)
			}
			content, _ := json.Marshal("{\"items\":[" + strings.Join(items, ",") + "]}")
			io.WriteString(w, `{"choices":[{"message":{"content":`+string(content)+`}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestClassifierEndToEnd(t *testing.T) {
	t.Setenv("STUDYTRACKER_AI_KEY", "test-key")
	var calls atomic.Int32
	llm := fakeLLM(t, &calls)
	defer llm.Close()

	dir := t.TempDir()
	cfg, _ := LoadConfig(filepath.Join(dir, "config.json"))
	if err := cfg.Update(func(c *Config) {
		c.AI = AIConfig{Enabled: true, BaseURL: llm.URL + "/v1", Model: "a-model"}
	}); err != nil {
		t.Fatal(err)
	}

	e := NewRuleEngine(writeRules(t, testRules))
	e.Reload()
	store, err := OpenStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Now().Unix()
	for _, s := range []session{
		{start: now - 900, end: now - 300, app: "chrome.exe", url: "https://www.youtube.com/watch?v=g", title: "Gradient Descent, Step-by-Step - YouTube"},
		{start: now - 300, end: now - 100, app: "chrome.exe", url: "https://www.instagram.com/", title: "Instagram"}, // rules: Distractions -> skipped
		{start: now - 100, end: now - 90, app: "notepad.exe", title: "tiny"},                                         // < 60s -> skipped
	} {
		s := s
		s.category = e.Categorize(s.app, s.title, s.url)
		if _, err := store.Insert(&s); err != nil {
			t.Fatal(err)
		}
	}

	labels := NewLabelCache()
	c := NewClassifier(store, e, labels, cfg, log.New(io.Discard, "", 0))

	pending, err := c.pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || !strings.Contains(pending[0].Title, "Gradient") {
		t.Fatalf("pending = %+v, want only the lecture", pending)
	}

	c.runOnce(context.Background())
	st := c.Status()
	if st.LastError != "" || st.Labeled != 1 || st.Pending != 0 || !st.Configured {
		t.Fatalf("status = %+v", st)
	}
	l, ok := labels.Get(pending[0].Key)
	if !ok || l.Subject != "Machine Learning" || l.Topic != "Gradient Descent" || l.Kind != kindProductive {
		t.Fatalf("label = %+v (ok=%v)", l, ok)
	}
	saved, _ := store.Labels()
	if len(saved) != 1 {
		t.Fatalf("labels persisted = %d, want 1", len(saved))
	}
	if calls.Load() != 2 { // first attempt rejected (response_format), retried plain
		t.Fatalf("chat calls = %d, want 2", calls.Load())
	}

	// Already labeled: a second run sends nothing.
	c.runOnce(context.Background())
	if calls.Load() != 2 {
		t.Fatalf("relabeled an already-labeled activity")
	}

	results, err := c.Test(context.Background())
	if err != nil || len(results) != 2 || results[1]["kind"] != kindDistraction {
		t.Fatalf("Test() = %+v, %v", results, err)
	}

	models, err := c.Models(context.Background())
	if err != nil || len(models) != 2 || models[0] != "a-model" || models[1] != "b-model" {
		t.Fatalf("Models() = %v, %v", models, err)
	}

	if err := c.Clear(); err != nil || labels.Len() != 0 {
		t.Fatalf("Clear() left %d labels (%v)", labels.Len(), err)
	}
}

// A provider that is "experiencing high demand" (503) for a while
// must be retried, not reported as a failure.
func TestRetriesTransientErrors(t *testing.T) {
	saved := retryDelays
	retryDelays = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	defer func() { retryDelays = saved }()

	var calls, failFirst atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n <= failFirst.Load() {
			w.Header().Set("Retry-After", "0")
			http.Error(w, `{"error":{"message":"This model is currently experiencing high demand."}}`, http.StatusServiceUnavailable)
			return
		}
		io.WriteString(w, `{"choices":[{"message":{"content":"{\"items\":[{\"id\":1,\"kind\":\"productive\",\"subject\":\"Statistics\"}]}"}}]}`)
	}))
	defer srv.Close()
	cfg := AIConfig{BaseURL: srv.URL, Model: "m"}

	// Two 503s, then success: the caller never sees an error.
	failFirst.Store(2)
	content, err := chatComplete(context.Background(), srv.Client(), cfg, "k", "sys", "user")
	if err != nil || !strings.Contains(content, "Statistics") {
		t.Fatalf("after transient 503s: content=%q err=%v", content, err)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3", calls.Load())
	}

	// Always overloaded: give up after the retries, with a helpful hint.
	calls.Store(0)
	failFirst.Store(100)
	_, err = chatComplete(context.Background(), srv.Client(), cfg, "k", "sys", "user")
	if err == nil || !strings.Contains(err.Error(), "high demand") || !strings.Contains(err.Error(), "retry automatically") {
		t.Fatalf("persistent 503 error = %v", err)
	}
	if calls.Load() != int32(len(retryDelays)+1) {
		t.Fatalf("calls = %d, want %d", calls.Load(), len(retryDelays)+1)
	}
}

// The Anthropic Messages API is a different protocol than
// OpenAI's: /v1/messages, x-api-key + anthropic-version headers,
// system at top level, max_tokens required, reply in content[].
func TestAnthropicProtocol(t *testing.T) {
	saved := retryDelays
	retryDelays = []time.Duration{time.Millisecond, time.Millisecond}
	defer func() { retryDelays = saved }()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			if r.Header.Get("x-api-key") != "sk-ant-test" || r.Header.Get("anthropic-version") == "" {
				http.Error(w, `{"type":"error","error":{"type":"authentication_error","message":"bad key"}}`, http.StatusUnauthorized)
				return
			}
			io.WriteString(w, `{"data":[{"id":"claude-haiku-4-5"},{"id":"claude-sonnet-5-5"}],"has_more":false}`)
		case "/v1/messages":
			if r.Header.Get("x-api-key") != "sk-ant-test" || r.Header.Get("anthropic-version") != anthropicVersion {
				t.Errorf("missing auth headers: %v", r.Header)
				http.Error(w, `{"type":"error","error":{"type":"authentication_error","message":"bad key"}}`, http.StatusUnauthorized)
				return
			}
			if calls.Add(1) == 1 { // transient overload, must be retried
				http.Error(w, `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`, http.StatusServiceUnavailable)
				return
			}
			raw, _ := io.ReadAll(r.Body)
			var body struct {
				Model     string              `json:"model"`
				MaxTokens int                 `json:"max_tokens"`
				System    string              `json:"system"`
				Messages  []map[string]string `json:"messages"`
			}
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Errorf("bad body: %v", err)
				return
			}
			if body.Model != "claude-haiku-4-5" || body.MaxTokens == 0 || body.System == "" ||
				len(body.Messages) != 1 || body.Messages[0]["role"] != "user" || body.Messages[0]["content"] == "" {
				t.Errorf("unexpected body: %+v", body)
			}
			text := "```json\n{\"items\":[{\"id\":1,\"kind\":\"productive\",\"category\":\"Lecture\"," +
				"\"subject\":\"Machine Learning\",\"topic\":\"Gradient Descent\"}]}\n```"
			io.WriteString(w, `{"content":[{"type":"thinking","thinking":"hmm"},{"type":"text","text":`+
				strconv.Quote(text)+`}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	if !anthropicStyle(AIConfig{Provider: "anthropic"}) ||
		!anthropicStyle(AIConfig{BaseURL: "https://api.anthropic.com/v1"}) ||
		anthropicStyle(AIConfig{Provider: "gemini", BaseURL: "https://x/v1"}) {
		t.Fatal("anthropicStyle detection broken")
	}

	cfg := AIConfig{Provider: "anthropic", BaseURL: srv.URL + "/v1", Model: "claude-haiku-4-5"}
	content, err := chatComplete(context.Background(), srv.Client(), cfg, "sk-ant-test", "label things", "classify")
	if err != nil {
		t.Fatalf("chatComplete: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2 (retried the 503)", calls.Load())
	}
	items, err := parseLabels(content)
	if err != nil || len(items) != 1 || items[0].Subject != "Machine Learning" || items[0].label().Kind != kindProductive {
		t.Fatalf("parsed %+v, %v", items, err)
	}

	// Load models through the same path Settings uses.
	dir := t.TempDir()
	cfgStore, _ := LoadConfig(filepath.Join(dir, "config.json"))
	if err := cfgStore.Update(func(c *Config) { c.AI = cfg }); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STUDYTRACKER_AI_KEY", "sk-ant-test")
	cl := NewClassifier(nil, nil, nil, cfgStore, log.New(io.Discard, "", 0))
	models, err := cl.Models(context.Background())
	if err != nil || len(models) != 2 || models[0] != "claude-haiku-4-5" || models[1] != "claude-sonnet-5-5" {
		t.Fatalf("Models() = %v, %v", models, err)
	}
}

// The Gemini free-tier quota error must be understood: parse the
// reset time, don't retry for 18 hours, and tell the user plainly.
func TestQuotaResetHint(t *testing.T) {
	gemini := errors.New(`429 Too Many Requests: You exceeded your current quota, please check your plan and billing details. ` +
		`For more information on this error, head to: https://ai.google.dev/gemini-api/docs/rate-limits. ` +
		`* Quota exceeded for metric: generativelanguage.googleapis.com/generate_content_free_tier_requests, ` +
		`limit: 20, model: gemini-3.8-flash Please retry in 18h11m44.841786254s.`)

	d, ok := quotaReset(gemini)
	if !ok || d < 18*time.Hour+11*time.Minute || d > 18*time.Hour+12*time.Minute {
		t.Fatalf("quotaReset = %v, %v — want ~18h11m", d, ok)
	}
	if d2, ok2 := quotaReset(errors.New("server busy, please retry in 45s")); !ok2 || d2 != 45*time.Second {
		t.Fatalf("short hint: %v, %v", d2, ok2)
	}
	if _, ok := quotaReset(errors.New("connection refused")); ok {
		t.Fatal("false positive on unrelated error")
	}
	if _, ok := quotaReset(nil); ok {
		t.Fatal("nil error should have no hint")
	}

	saved := retryDelays
	retryDelays = []time.Duration{time.Millisecond, time.Millisecond}
	defer func() { retryDelays = saved }()

	// Hours-away quota: exactly one attempt, no futility.
	var n atomic.Int32
	_, status, err := retrying(context.Background(), func() (string, int, time.Duration, error) {
		n.Add(1)
		return "", http.StatusTooManyRequests, 0, gemini
	})
	if n.Load() != 1 || status != http.StatusTooManyRequests || err == nil {
		t.Fatalf("long quota: n=%d status=%d err=%v", n.Load(), status, err)
	}

	// A short hint (<= 1 min) is still worth retrying.
	n.Store(0)
	content, _, err := retrying(context.Background(), func() (string, int, time.Duration, error) {
		if n.Add(1) == 1 {
			return "", http.StatusTooManyRequests, 0, errors.New("retry in 45s")
		}
		return "ok", http.StatusOK, 0, nil
	})
	if err != nil || content != "ok" || n.Load() != 2 {
		t.Fatalf("short quota hint: content=%q n=%d err=%v", content, n.Load(), err)
	}

	// The surfaced message explains the pause instead of promising retries.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":{"message":"`+strings.ReplaceAll(gemini.Error(), `"`, "'")+`"}}`)
	}))
	defer srv.Close()
	cfg := AIConfig{Provider: "gemini", BaseURL: srv.URL, Model: "gemini-3.8-flash"}
	_, err = chatComplete(context.Background(), srv.Client(), cfg, "k", "sys", "user")
	if err == nil || !strings.Contains(err.Error(), "free-tier daily quota") ||
		!strings.Contains(err.Error(), "18h") || !strings.Contains(err.Error(), "switch provider") {
		t.Fatalf("message = %v", err)
	}
}

func TestSettingsValidation(t *testing.T) {
	bad := []string{
		"503 Service Unavailable: This model is currently experiencing high demand. Please try again later.",
		"short",
		"https://aistudio.google.com/apikey extra",
	}
	for _, k := range bad {
		if looksLikeAPIKey(k) {
			t.Errorf("accepted bogus key %q", k)
		}
	}
	if !looksLikeAPIKey("AIzaSyD-abcdefghijklmnopqrstuvwxyz01234") || !looksLikeAPIKey("sk-proj-abc_DEF-123456") {
		t.Error("rejected a real-looking key")
	}
	for _, m := range []string{"gemini-3.8-flash-lite-tts", "gemini-embedding-001", "veo-3.1-generate-preview", "gemini-2.5-flash-image"} {
		if !isNonChatModel(m) {
			t.Errorf("%s should be rejected as non-chat", m)
		}
	}
	for _, m := range []string{"gemini-3.8-flash", "gemini-3.8-flash-lite", "gpt-5-mini", "llama3.2"} {
		if isNonChatModel(m) {
			t.Errorf("%s wrongly rejected", m)
		}
	}
}

// The API key must never be stored verbatim: DPAPI on Windows, mode
// 0600 + a format prefix on Linux. Either way it survives a reload.
func TestAPIKeyAtRest(t *testing.T) {
	t.Setenv("STUDYTRACKER_AI_KEY", "")
	path := filepath.Join(t.TempDir(), "config.json")
	cfg, _ := LoadConfig(path)
	enc, err := encryptAPIKey("AIza-secret-1234")
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Update(func(c *Config) { c.AI.APIKeyEnc = enc }); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.APIKey(); got != "AIza-secret-1234" {
		t.Fatalf("APIKey() = %q", got)
	}
	if strings.Contains(reloaded.Get().AI.APIKeyEnc, "secret") {
		t.Fatal("key stored in plain text")
	}
	if keyHint("AIza-secret-1234") != "••••1234" {
		t.Fatalf("keyHint = %q", keyHint("AIza-secret-1234"))
	}
}
