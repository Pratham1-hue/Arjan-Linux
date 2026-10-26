package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Server serves the dashboard (static files built by Next.js) and
// its JSON API on 127.0.0.1 only.
//
// Security: any website you visit could try to talk to
// localhost. Requests must carry a localhost Host header (blocks
// DNS rebinding) and state-changing requests need an X-Arjan
// header, which a cross-site page cannot send without a CORS
// preflight that this server never approves.
type Server struct {
	analyzer  *Analyzer
	tracker   *Tracker
	cfg       *ConfigStore
	ai        *Classifier
	webDir    string
	dataDir   string
	rulesPath string
	icon      []byte
	lg        *log.Logger

	srv  *http.Server
	addr string
}

func (s *Server) Start() error {
	port := s.cfg.Get().Port
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		s.lg.Printf("port %d busy (%v); using a random free port", port, err)
		if ln, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
			return err
		}
	}
	s.addr = ln.Addr().String()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/stats", s.handleStats)
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("POST /api/tracking", s.handleTracking)
	mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	mux.HandleFunc("POST /api/settings", s.handleSaveSettings)
	mux.HandleFunc("POST /api/ai/test", s.handleAITest)
	mux.HandleFunc("POST /api/ai/run", s.handleAIRun)
	mux.HandleFunc("POST /api/ai/clear", s.handleAIClear)
	mux.HandleFunc("GET /api/ai/models", s.handleAIModels)
	mux.HandleFunc("GET /favicon.ico", s.handleIcon)
	mux.Handle("/", s.static())

	s.srv = &http.Server{Handler: s.guard(mux), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := s.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.lg.Printf("http server: %v", err)
		}
	}()
	s.lg.Printf("dashboard at %s", s.URL())
	return nil
}

func (s *Server) URL() string { return "http://" + s.addr }

func (s *Server) Shutdown() {
	if s.srv == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.srv.Shutdown(ctx)
}

func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if host != "localhost" && host != "127.0.0.1" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("X-Arjan") != "1" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

const notBuiltHTML = `<!doctype html><html><body style="background:#09090b;color:#e4e4e7;font-family:system-ui;padding:40px">
<h2>Arjan is running, but the dashboard isn't built yet.</h2>
<p>Run <code>./build.sh</code> (Linux) or <code>build.ps1</code> (Windows) in the project folder — it builds the dashboard into <code>dist/web</code>.</p>
</body></html>`

func (s *Server) static() http.Handler {
	files := http.FileServer(http.Dir(s.webDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := os.Stat(filepath.Join(s.webDir, "index.html")); err != nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.WriteString(w, notBuiltHTML)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/_next/static/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}

func (s *Server) handleIcon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/x-icon")
	w.Write(s.icon)
}

// ------------------------------------------------------------------ API ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(v)
}

type statsResponse struct {
	Date    string     `json:"date"`
	IsToday bool       `json:"isToday"`
	Paused  bool       `json:"paused"`
	AI      AIStatus   `json:"ai"`
	Day     *DayReport `json:"day"`
	Week    []WeekDay  `json:"week"`
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	day := now
	if q := r.URL.Query().Get("date"); q != "" {
		d, err := time.ParseInLocation("2006-01-02", q, time.Local)
		if err != nil {
			writeErr(w, http.StatusBadRequest, errors.New("date must be YYYY-MM-DD"))
			return
		}
		if d.Before(now) {
			day = d
		}
	}
	rep, err := s.analyzer.Day(day)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	week, err := s.analyzer.Week(day)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, statsResponse{
		Date:    day.Format("2006-01-02"),
		IsToday: day.Format("2006-01-02") == now.Format("2006-01-02"),
		Paused:  s.tracker.Paused(),
		AI:      s.ai.Status(),
		Day:     rep,
		Week:    week,
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"paused":       s.tracker.Paused(),
		"ai":           s.ai.Status(),
		"dashboardURL": s.URL(),
	})
}

func (s *Server) handleTracking(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Paused bool `json:"paused"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.tracker.SetPaused(req.Paused)
	writeJSON(w, http.StatusOK, map[string]bool{"paused": s.tracker.Paused()})
}

type settingsAI struct {
	Enabled  bool   `json:"enabled"`
	Provider string `json:"provider"`
	BaseURL  string `json:"baseURL"`
	Model    string `json:"model"`
	HasKey   bool   `json:"hasKey"`
	KeyHint  string `json:"keyHint"`
}

type settingsResponse struct {
	AI               settingsAI `json:"ai"`
	StartWithWindows bool       `json:"startWithWindows"`
	DataDir          string     `json:"dataDir"`
	RulesPath        string     `json:"rulesPath"`
	DashboardURL     string     `json:"dashboardURL"`
}

func (s *Server) settings() settingsResponse {
	cfg := s.cfg.Get()
	key := s.cfg.APIKey()
	return settingsResponse{
		AI: settingsAI{
			Enabled: cfg.AI.Enabled, Provider: cfg.AI.Provider,
			BaseURL: cfg.AI.BaseURL, Model: cfg.AI.Model,
			HasKey: key != "", KeyHint: keyHint(key),
		},
		StartWithWindows: autostartEnabled(),
		DataDir:          s.dataDir,
		RulesPath:        s.rulesPath,
		DashboardURL:     s.URL(),
	}
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.settings())
}

func (s *Server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AI *struct {
			Enabled  *bool   `json:"enabled"`
			Provider *string `json:"provider"`
			BaseURL  *string `json:"baseURL"`
			Model    *string `json:"model"`
			APIKey   *string `json:"apiKey"`
			ClearKey bool    `json:"clearKey"`
		} `json:"ai"`
		StartWithWindows *bool `json:"startWithWindows"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	if a := req.AI; a != nil {
		if a.BaseURL != nil {
			if u := strings.TrimSpace(*a.BaseURL); u != "" &&
				!strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
				writeErr(w, http.StatusBadRequest, errors.New("base URL must start with http:// or https://"))
				return
			}
		}
		if a.Model != nil && isNonChatModel(*a.Model) {
			writeErr(w, http.StatusBadRequest, fmt.Errorf(
				"%q is a speech/image/embedding model and can't classify text — choose a regular chat model (e.g. a \"flash\" model)",
				strings.TrimSpace(*a.Model)))
			return
		}
		var enc string
		if a.APIKey != nil && strings.TrimSpace(*a.APIKey) != "" {
			if !looksLikeAPIKey(strings.TrimSpace(*a.APIKey)) {
				writeErr(w, http.StatusBadRequest, errors.New(
					"that doesn't look like an API key (it contains spaces or is too long) — paste only the key itself"))
				return
			}
			var err error
			if enc, err = encryptAPIKey(strings.TrimSpace(*a.APIKey)); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
		}
		err := s.cfg.Update(func(c *Config) {
			if a.Enabled != nil {
				c.AI.Enabled = *a.Enabled
			}
			if a.Provider != nil {
				c.AI.Provider = strings.TrimSpace(*a.Provider)
			}
			if a.BaseURL != nil {
				c.AI.BaseURL = strings.TrimRight(strings.TrimSpace(*a.BaseURL), "/")
			}
			if a.Model != nil {
				c.AI.Model = strings.TrimSpace(*a.Model)
			}
			if enc != "" {
				c.AI.APIKeyEnc = enc
			}
			if a.ClearKey {
				c.AI.APIKeyEnc = ""
			}
		})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		// Settings changed: resume immediately (the old provider's
		// quota pause must not block the new one) and try a run.
		s.ai.ResetBackoff()
		if s.cfg.Get().AI.Enabled {
			s.ai.Trigger()
		}
	}

	if req.StartWithWindows != nil {
		if err := setAutostart(*req.StartWithWindows); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, s.settings())
}

func (s *Server) handleAITest(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	results, err := s.ai.Test(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"results":   results,
		"latencyMs": time.Since(start).Milliseconds(),
	})
}

func (s *Server) handleAIRun(w http.ResponseWriter, r *http.Request) {
	s.ai.Trigger()
	writeJSON(w, http.StatusOK, s.ai.Status())
}

func (s *Server) handleAIClear(w http.ResponseWriter, r *http.Request) {
	if err := s.ai.Clear(); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, s.ai.Status())
}

func (s *Server) handleAIModels(w http.ResponseWriter, r *http.Request) {
	ids, err := s.ai.Models(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": ids})
}
