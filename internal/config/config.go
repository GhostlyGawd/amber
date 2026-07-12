// Package config loads and persists Amber configuration and resolves store
// locations (global ~/.amber, project ./.amber).
//
// Precedence: env vars (AMBER_*) > project config > global config > defaults.
// The config file never contains secrets — API keys are referenced by env
// var name only.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Scope identifies which store a command operates on.
type Scope string

const (
	ScopeGlobal  Scope = "global"
	ScopeProject Scope = "project"
	ScopeAll     Scope = "all" // recall-only: query both and fuse
)

// DBFileName is the SQLite file inside a store directory.
const DBFileName = "amber.db"

// ConfigFileName is the JSON config inside a store directory.
const ConfigFileName = "config.json"

// Config is the persisted configuration for one store.
type Config struct {
	Embedding   Embedding   `json:"embedding"`
	Digest      Digest      `json:"digest"`
	Scan        Scan        `json:"scan"`
	Inject      Inject      `json:"inject"`
	Recall      Recall      `json:"recall"`
	Consolidate Consolidate `json:"consolidate"`
	Telemetry   Telemetry   `json:"telemetry"`
	Store       StoreMeta   `json:"store"`
}

// Embedding selects the embedding provider.
//
// Providers:
//   - "local":         pure-Go inference over a Model2Vec-class static model
//   - "openai-compat": any OpenAI-compatible /v1/embeddings endpoint (opt-in)
//   - "hash":          deterministic token-hash embedder (offline tests/dev)
//   - "none":          BM25-only floor — no vectors, lexical recall only
type Embedding struct {
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Dims      int    `json:"dims"`
	Endpoint  string `json:"endpoint,omitempty"`
	APIKeyEnv string `json:"api_key_env,omitempty"`
	// ModelURL overrides where `local` model files are fetched from.
	ModelURL string `json:"model_url,omitempty"`
}

// Digest configures LLM extraction.
//
// Backends:
//   - "auto":          claude CLI if on PATH, else anthropic API if key set, else none
//   - "claude-cli":    shell out to `claude -p`
//   - "anthropic-api": Messages API using the env var named by APIKeyEnv
//   - "cmd":           arbitrary command reading prompt on stdin, writing JSON to stdout
//   - "none":          digest disabled
type Digest struct {
	Backend   string `json:"backend"`
	Command   string `json:"command,omitempty"` // for backend=cmd
	Model     string `json:"model,omitempty"`   // for anthropic-api
	APIKeyEnv string `json:"api_key_env,omitempty"`
	// Posture: "review-first" (auto-digested memories land in the review
	// inbox) or "auto" (T2 goes active immediately; T3 always quarantined).
	// Default review-first for a store's first two weeks (decision F3).
	Posture string `json:"posture"`
	// MinTranscriptChars: SessionEnd digests skip transcripts shorter than this.
	MinTranscriptChars int `json:"min_transcript_chars"`
	// ImportanceFloor: hook-triggered digests drop candidates below this.
	ImportanceFloor int `json:"importance_floor"`
}

// Scan configures the secret/PII scanner. Mode: "warn" (refuse unless
// --force; secrets stored redacted) or "block" (never store).
type Scan struct {
	Mode string `json:"mode"`
}

// Inject bounds SessionStart context injection.
type Inject struct {
	BudgetTokens int `json:"budget_tokens"`
	MaxItems     int `json:"max_items"`
}

// Recall defaults.
type Recall struct {
	Limit int `json:"limit"`
}

// Consolidate schedule posture. Amber never runs consolidation without
// opt-in (decision D16).
type Consolidate struct {
	Auto bool `json:"auto"`
}

// Telemetry is zero by default. Counters-only stats exist locally; nothing
// is ever sent unless the user opted in AND configured an endpoint AND
// confirms the printed payload. v1 ships with no endpoint.
type Telemetry struct {
	Counters bool   `json:"counters"`
	Endpoint string `json:"endpoint,omitempty"`
}

// StoreMeta records store-level facts used for posture decisions.
type StoreMeta struct {
	CreatedAt string `json:"created_at,omitempty"`
}

// Default returns the documented defaults.
func Default() *Config {
	return &Config{
		Embedding: Embedding{Provider: "none", Model: "", Dims: 0},
		Digest: Digest{
			Backend:            "auto",
			Model:              "claude-sonnet-5",
			APIKeyEnv:          "ANTHROPIC_API_KEY",
			Posture:            "review-first",
			MinTranscriptChars: 2000,
			ImportanceFloor:    2,
		},
		Scan:        Scan{Mode: "warn"},
		Inject:      Inject{BudgetTokens: 700, MaxItems: 12},
		Recall:      Recall{Limit: 8},
		Consolidate: Consolidate{Auto: false},
		Telemetry:   Telemetry{Counters: false},
	}
}

// HomeDir returns the global Amber directory (~/.amber or $AMBER_HOME).
func HomeDir() (string, error) {
	if h := os.Getenv("AMBER_HOME"); h != "" {
		return h, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".amber"), nil
}

// ModelsDir returns where local embedding models are cached (always global,
// shared across stores).
func ModelsDir() (string, error) {
	h, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "models"), nil
}

// FindProjectDir walks up from dir looking for a .amber directory.
// Returns "" if none found.
func FindProjectDir(dir string) string {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		cand := filepath.Join(dir, ".amber")
		if fi, err := os.Stat(cand); err == nil && fi.IsDir() {
			return cand
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// ResolveStoreDir picks the store directory for the requested scope.
// scope "" means: project store if one exists upward from cwd, else global.
func ResolveStoreDir(scope Scope, cwd string) (dir string, resolved Scope, err error) {
	if env := os.Getenv("AMBER_STORE"); env != "" && scope == "" {
		return env, ScopeGlobal, nil
	}
	switch scope {
	case ScopeProject:
		p := FindProjectDir(cwd)
		if p == "" {
			return "", "", errors.New("no project store found (run `amber init --project` in the project root)")
		}
		return p, ScopeProject, nil
	case ScopeGlobal:
		g, err := HomeDir()
		if err != nil {
			return "", "", err
		}
		return g, ScopeGlobal, nil
	case "", ScopeAll:
		if p := FindProjectDir(cwd); p != "" {
			return p, ScopeProject, nil
		}
		g, err := HomeDir()
		if err != nil {
			return "", "", err
		}
		return g, ScopeGlobal, nil
	default:
		return "", "", fmt.Errorf("unknown scope %q (want global|project|all)", scope)
	}
}

// Load reads the config for a store directory, applying defaults for
// missing fields and env overrides. Missing file returns defaults.
func Load(storeDir string) (*Config, error) {
	cfg := Default()
	path := filepath.Join(storeDir, ConfigFileName)
	b, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(b, cfg); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	applyEnv(cfg)
	normalize(cfg)
	return cfg, nil
}

// Save writes the config with 0600 permissions.
func Save(storeDir string, cfg *Config) error {
	if err := os.MkdirAll(storeDir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(storeDir, ConfigFileName)
	return writeFile0600(path, append(b, '\n'))
}

func writeFile0600(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("AMBER_EMBED_PROVIDER"); v != "" {
		cfg.Embedding.Provider = v
	}
	if v := os.Getenv("AMBER_EMBED_MODEL"); v != "" {
		cfg.Embedding.Model = v
	}
	if v := os.Getenv("AMBER_EMBED_ENDPOINT"); v != "" {
		cfg.Embedding.Endpoint = v
	}
	if v := os.Getenv("AMBER_DIGEST_BACKEND"); v != "" {
		cfg.Digest.Backend = v
	}
	if v := os.Getenv("AMBER_DIGEST_CMD"); v != "" {
		cfg.Digest.Backend = "cmd"
		cfg.Digest.Command = v
	}
	if v := os.Getenv("AMBER_DIGEST_POSTURE"); v != "" {
		cfg.Digest.Posture = v
	}
	if v := os.Getenv("AMBER_SCAN_MODE"); v != "" {
		cfg.Scan.Mode = v
	}
	if v := os.Getenv("AMBER_INJECT_BUDGET"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Inject.BudgetTokens = n
		}
	}
}

func normalize(cfg *Config) {
	if cfg.Inject.BudgetTokens <= 0 {
		cfg.Inject.BudgetTokens = 700
	}
	if cfg.Inject.MaxItems <= 0 {
		cfg.Inject.MaxItems = 12
	}
	if cfg.Recall.Limit <= 0 {
		cfg.Recall.Limit = 8
	}
	if cfg.Digest.MinTranscriptChars <= 0 {
		cfg.Digest.MinTranscriptChars = 2000
	}
	if cfg.Digest.Posture == "" {
		cfg.Digest.Posture = "review-first"
	}
	if cfg.Scan.Mode == "" {
		cfg.Scan.Mode = "warn"
	}
	if cfg.Digest.Backend == "" {
		cfg.Digest.Backend = "auto"
	}
}

// StoreAge returns how old the store is, for the review-first → auto
// posture suggestion (F3). Zero time if unknown.
func (c *Config) StoreAge(now time.Time) time.Duration {
	if c.Store.CreatedAt == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, c.Store.CreatedAt)
	if err != nil {
		return 0
	}
	return now.Sub(t)
}

// --- flat key get/set for `amber config` ---

// Keys returns all settable dotted keys in stable order.
func Keys() []string {
	ks := []string{
		"embedding.provider", "embedding.model", "embedding.dims", "embedding.endpoint", "embedding.api_key_env", "embedding.model_url",
		"digest.backend", "digest.command", "digest.model", "digest.api_key_env", "digest.posture", "digest.min_transcript_chars", "digest.importance_floor",
		"scan.mode",
		"inject.budget_tokens", "inject.max_items",
		"recall.limit",
		"consolidate.auto",
		"telemetry.counters", "telemetry.endpoint",
	}
	sort.Strings(ks)
	return ks
}

// Get returns the value of a dotted key as a string.
func (c *Config) Get(key string) (string, error) {
	switch key {
	case "embedding.provider":
		return c.Embedding.Provider, nil
	case "embedding.model":
		return c.Embedding.Model, nil
	case "embedding.dims":
		return strconv.Itoa(c.Embedding.Dims), nil
	case "embedding.endpoint":
		return c.Embedding.Endpoint, nil
	case "embedding.api_key_env":
		return c.Embedding.APIKeyEnv, nil
	case "embedding.model_url":
		return c.Embedding.ModelURL, nil
	case "digest.backend":
		return c.Digest.Backend, nil
	case "digest.command":
		return c.Digest.Command, nil
	case "digest.model":
		return c.Digest.Model, nil
	case "digest.api_key_env":
		return c.Digest.APIKeyEnv, nil
	case "digest.posture":
		return c.Digest.Posture, nil
	case "digest.min_transcript_chars":
		return strconv.Itoa(c.Digest.MinTranscriptChars), nil
	case "digest.importance_floor":
		return strconv.Itoa(c.Digest.ImportanceFloor), nil
	case "scan.mode":
		return c.Scan.Mode, nil
	case "inject.budget_tokens":
		return strconv.Itoa(c.Inject.BudgetTokens), nil
	case "inject.max_items":
		return strconv.Itoa(c.Inject.MaxItems), nil
	case "recall.limit":
		return strconv.Itoa(c.Recall.Limit), nil
	case "consolidate.auto":
		return strconv.FormatBool(c.Consolidate.Auto), nil
	case "telemetry.counters":
		return strconv.FormatBool(c.Telemetry.Counters), nil
	case "telemetry.endpoint":
		return c.Telemetry.Endpoint, nil
	}
	return "", fmt.Errorf("unknown config key %q (see `amber config` for keys)", key)
}

// Set assigns a dotted key from a string value, validating enums.
func (c *Config) Set(key, val string) error {
	boolVal := func() (bool, error) { return strconv.ParseBool(val) }
	intVal := func() (int, error) { return strconv.Atoi(val) }
	switch key {
	case "embedding.provider":
		switch val {
		case "local", "openai-compat", "hash", "none":
			c.Embedding.Provider = val
		default:
			return fmt.Errorf("embedding.provider must be local|openai-compat|hash|none")
		}
	case "embedding.model":
		c.Embedding.Model = val
	case "embedding.dims":
		n, err := intVal()
		if err != nil {
			return err
		}
		c.Embedding.Dims = n
	case "embedding.endpoint":
		c.Embedding.Endpoint = val
	case "embedding.api_key_env":
		c.Embedding.APIKeyEnv = val
	case "embedding.model_url":
		c.Embedding.ModelURL = val
	case "digest.backend":
		switch val {
		case "auto", "claude-cli", "anthropic-api", "cmd", "none":
			c.Digest.Backend = val
		default:
			return fmt.Errorf("digest.backend must be auto|claude-cli|anthropic-api|cmd|none")
		}
	case "digest.command":
		c.Digest.Command = val
	case "digest.model":
		c.Digest.Model = val
	case "digest.api_key_env":
		c.Digest.APIKeyEnv = val
	case "digest.posture":
		switch val {
		case "review-first", "auto":
			c.Digest.Posture = val
		default:
			return fmt.Errorf("digest.posture must be review-first|auto")
		}
	case "digest.min_transcript_chars":
		n, err := intVal()
		if err != nil {
			return err
		}
		c.Digest.MinTranscriptChars = n
	case "digest.importance_floor":
		n, err := intVal()
		if err != nil {
			return err
		}
		c.Digest.ImportanceFloor = n
	case "scan.mode":
		switch val {
		case "warn", "block":
			c.Scan.Mode = val
		default:
			return fmt.Errorf("scan.mode must be warn|block")
		}
	case "inject.budget_tokens":
		n, err := intVal()
		if err != nil {
			return err
		}
		c.Inject.BudgetTokens = n
	case "inject.max_items":
		n, err := intVal()
		if err != nil {
			return err
		}
		c.Inject.MaxItems = n
	case "recall.limit":
		n, err := intVal()
		if err != nil {
			return err
		}
		c.Recall.Limit = n
	case "consolidate.auto":
		b, err := boolVal()
		if err != nil {
			return err
		}
		c.Consolidate.Auto = b
	case "telemetry.counters":
		b, err := boolVal()
		if err != nil {
			return err
		}
		c.Telemetry.Counters = b
	case "telemetry.endpoint":
		c.Telemetry.Endpoint = val
	default:
		return fmt.Errorf("unknown config key %q", key)
	}
	return nil
}

// ParseSince parses durations like "30d", "12h", "2w" for --since flags.
func ParseSince(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	unit := s[len(s)-1]
	numPart := s[:len(s)-1]
	switch unit {
	case 'd', 'w':
		n, err := strconv.ParseFloat(numPart, 64)
		if err != nil {
			return 0, fmt.Errorf("bad duration %q (want e.g. 30d, 2w, 12h)", s)
		}
		if unit == 'd' {
			return time.Duration(n * 24 * float64(time.Hour)), nil
		}
		return time.Duration(n * 7 * 24 * float64(time.Hour)), nil
	default:
		d, err := time.ParseDuration(s)
		if err != nil {
			return 0, fmt.Errorf("bad duration %q (want e.g. 30d, 2w, 12h)", s)
		}
		return d, nil
	}
}
