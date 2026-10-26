package main

import (
	"encoding/json"
	"errors"
	"os"
	"sync"
)

const defaultPort = 4000

// AIConfig selects an OpenAI-compatible chat completions endpoint.
// One code path covers Gemini, OpenAI, OpenRouter, Groq and a
// local Ollama.
type AIConfig struct {
	Enabled  bool   `json:"enabled"`
	Provider string `json:"provider"` // preset id shown in the UI
	BaseURL  string `json:"baseURL"`
	Model    string `json:"model"`
	// APIKeyEnc is the API key as stored on disk: DPAPI-encrypted on
	// Windows (only this Windows user can decrypt it), base64 with a
	// "plain:" prefix on Linux (config.json is mode 0600). Never logged.
	APIKeyEnc string `json:"apiKeyEnc,omitempty"`
}

// Config lives in the per-user data folder:
// %LOCALAPPDATA%\StudyTracker\config.json on Windows,
// ~/.local/share/arjan/config.json on Linux.
type Config struct {
	Port int      `json:"port"`
	AI   AIConfig `json:"ai"`
}

func defaultConfig() Config {
	return Config{
		Port: defaultPort,
		AI: AIConfig{
			Provider: "gemini",
			BaseURL:  "https://generativelanguage.googleapis.com/v1beta/openai",
			Model:    "gemini-3.8-flash",
		},
	}
}

// ConfigStore is a thread-safe, file-backed Config.
type ConfigStore struct {
	path string
	mu   sync.RWMutex
	cfg  Config
}

// LoadConfig reads path, falling back to defaults when the file
// does not exist yet.
func LoadConfig(path string) (*ConfigStore, error) {
	cs := &ConfigStore{path: path, cfg: defaultConfig()}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cs, nil
	}
	if err != nil {
		return cs, err
	}
	if err := json.Unmarshal(data, &cs.cfg); err != nil {
		return cs, err
	}
	if cs.cfg.Port <= 0 {
		cs.cfg.Port = defaultPort
	}
	return cs, nil
}

func (cs *ConfigStore) Get() Config {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.cfg
}

// Update applies fn to a copy of the config and persists it
// atomically (write temp file, then rename).
func (cs *ConfigStore) Update(fn func(*Config)) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	next := cs.cfg
	fn(&next)
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	tmp := cs.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, cs.path); err != nil {
		return err
	}
	// Only the owner may read the settings (they can hold an API key).
	_ = os.Chmod(cs.path, 0o600)
	cs.cfg = next
	return nil
}

// APIKey returns the decrypted API key ("" if none).
// The STUDYTRACKER_AI_KEY environment variable takes precedence.
func (cs *ConfigStore) APIKey() string {
	if v := os.Getenv("STUDYTRACKER_AI_KEY"); v != "" {
		return v
	}
	enc := cs.Get().AI.APIKeyEnc
	if enc == "" {
		return ""
	}
	plain, err := unprotectAPIKey(enc)
	if err != nil {
		return ""
	}
	return plain
}

// keyHint is a masked version of the key for display.
func keyHint(key string) string {
	if key == "" {
		return ""
	}
	r := []rune(key)
	if len(r) <= 4 {
		return "••••"
	}
	return "••••" + string(r[len(r)-4:])
}

func encryptAPIKey(key string) (string, error) {
	if key == "" {
		return "", nil
	}
	return protectAPIKey(key)
}
