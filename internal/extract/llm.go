package extract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ghostlygawd/amber/internal/config"
)

// Backend runs one extraction prompt and returns raw model output.
type Backend interface {
	Name() string
	Complete(ctx context.Context, prompt string) (string, error)
}

// NewBackend resolves the configured digest backend (D6): `claude -p`
// when present; ANTHROPIC_API_KEY fallback; arbitrary command;
// configurable.
func NewBackend(cfg *config.Config) (Backend, error) {
	d := cfg.Digest
	switch d.Backend {
	case "none":
		return nil, fmt.Errorf("digest disabled (digest.backend=none)")
	case "claude-cli":
		return newClaudeCLI()
	case "anthropic-api":
		return newAnthropicAPI(d)
	case "cmd":
		if d.Command == "" {
			return nil, fmt.Errorf("digest.backend=cmd requires digest.command")
		}
		return &cmdBackend{command: d.Command}, nil
	case "auto", "":
		if b, err := newClaudeCLI(); err == nil {
			return b, nil
		}
		if b, err := newAnthropicAPI(d); err == nil {
			return b, nil
		}
		return nil, fmt.Errorf("no digest backend available: `claude` not on PATH and %s unset\n(set digest.backend, or install Claude Code)", keyEnvName(d))
	default:
		return nil, fmt.Errorf("unknown digest.backend %q", d.Backend)
	}
}

func keyEnvName(d config.Digest) string {
	if d.APIKeyEnv != "" {
		return d.APIKeyEnv
	}
	return "ANTHROPIC_API_KEY"
}

// --- claude CLI (`claude -p`) ---

type claudeCLI struct{ path string }

func newClaudeCLI() (*claudeCLI, error) {
	p, err := exec.LookPath("claude")
	if err != nil {
		return nil, fmt.Errorf("claude CLI not found on PATH")
	}
	return &claudeCLI{path: p}, nil
}

func (c *claudeCLI) Name() string { return "claude-cli" }

func (c *claudeCLI) Complete(ctx context.Context, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	// -p: print mode; prompt on stdin keeps arbitrary content out of argv.
	cmd := exec.CommandContext(ctx, c.path, "-p", "--output-format", "text")
	cmd.Stdin = strings.NewReader(prompt)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("claude -p failed: %v: %s", err, truncateStr(errb.String(), 400))
	}
	return out.String(), nil
}

// --- Anthropic Messages API ---

type anthropicAPI struct {
	key   string
	model string
}

func newAnthropicAPI(d config.Digest) (*anthropicAPI, error) {
	key := os.Getenv(keyEnvName(d))
	if key == "" {
		return nil, fmt.Errorf("%s not set", keyEnvName(d))
	}
	model := d.Model
	if model == "" {
		model = "claude-sonnet-5"
	}
	return &anthropicAPI{key: key, model: model}, nil
}

func (a *anthropicAPI) Name() string { return "anthropic-api/" + a.model }

func (a *anthropicAPI) Complete(ctx context.Context, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	body, _ := json.Marshal(map[string]any{
		"model":      a.model,
		"max_tokens": 4096,
		"messages":   []map[string]any{{"role": "user", "content": prompt}},
	})
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", a.key)
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("anthropic API HTTP %d: %s", resp.StatusCode, string(b))
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, c := range out.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	return sb.String(), nil
}

// --- arbitrary command (tests, local models, anything) ---

type cmdBackend struct{ command string }

func (c *cmdBackend) Name() string { return "cmd" }

func (c *cmdBackend) Complete(ctx context.Context, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", c.command)
	cmd.Stdin = strings.NewReader(prompt)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("digest command failed: %v: %s", err, truncateStr(errb.String(), 400))
	}
	return out.String(), nil
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
