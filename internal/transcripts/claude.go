// Package transcripts discovers and parses Claude Code session
// transcripts (~/.claude/projects/*/*.jsonl) for retroactive onboarding
// (decision D15) and SessionEnd digests.
//
// Taint marking (§9 defense 3): tool results and other non-dialogue spans
// are wrapped in sentinels BEFORE extraction, so anything derived from
// them lands in trust tier T3 (quarantined).
package transcripts

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// TaintOpen and TaintClose bracket untrusted-origin spans in the rendered
// transcript. The extraction prompt requires candidates derived from
// bracketed content to be marked tainted; the post-screen re-checks
// regardless (never trust the LLM to self-report).
const (
	TaintOpen  = "⟦UNTRUSTED⟧"
	TaintClose = "⟦/UNTRUSTED⟧"
)

// Session is one discovered transcript.
type Session struct {
	Path      string
	SessionID string
	Project   string // munged project dir name
	ModTime   time.Time
	SizeBytes int64
}

// Rendered is a transcript prepared for extraction.
type Rendered struct {
	SessionID    string
	Text         string   // dialogue with taint sentinels
	TaintedSpans []string // raw tainted span contents, for the post-screen
	Chars        int
	Turns        int
}

// ClaudeDir returns the Claude Code home (~/.claude or $AMBER_CLAUDE_DIR
// for tests).
func ClaudeDir() (string, error) {
	if d := os.Getenv("AMBER_CLAUDE_DIR"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

// Discover finds transcripts under <claudeDir>/projects modified within
// the window (0 = no limit), newest first.
func Discover(claudeDir string, within time.Duration) ([]Session, error) {
	root := filepath.Join(claudeDir, "projects")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	cutoff := time.Time{}
	if within > 0 {
		cutoff = time.Now().Add(-within)
	}
	var out []Session
	for _, proj := range entries {
		if !proj.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(root, proj.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			info, err := f.Info()
			if err != nil {
				continue
			}
			if !cutoff.IsZero() && info.ModTime().Before(cutoff) {
				continue
			}
			out = append(out, Session{
				Path:      filepath.Join(root, proj.Name(), f.Name()),
				SessionID: strings.TrimSuffix(f.Name(), ".jsonl"),
				Project:   proj.Name(),
				ModTime:   info.ModTime(),
				SizeBytes: info.Size(),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime.After(out[j].ModTime) })
	return out, nil
}

// line is the defensive shape of one Claude Code transcript JSONL row.
// The format is not a published spec; parse what is recognizable, skip
// the rest.
type line struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionId"`
	Message   struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	ToolUseResult json.RawMessage `json:"toolUseResult"`
}

type contentBlock struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Content json.RawMessage `json:"content"` // tool_result payload
	Name    string          `json:"name"`    // tool_use name
}

// Parse renders a transcript file: user/assistant dialogue verbatim,
// tool results wrapped in taint sentinels and truncated (they are context
// for the extractor, not content to memorize).
func Parse(path string) (*Rendered, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := &Rendered{}
	var b strings.Builder
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 8*1024*1024)
	for sc.Scan() {
		raw := sc.Bytes()
		if len(raw) == 0 {
			continue
		}
		var l line
		if err := json.Unmarshal(raw, &l); err != nil {
			continue
		}
		if r.SessionID == "" && l.SessionID != "" {
			r.SessionID = l.SessionID
		}
		role := l.Message.Role
		if role == "" {
			role = l.Type
		}
		if role != "user" && role != "assistant" {
			continue
		}
		text, tainted := renderContent(l.Message.Content)
		if strings.TrimSpace(text) == "" && len(tainted) == 0 {
			continue
		}
		r.Turns++
		fmt.Fprintf(&b, "%s: %s\n\n", role, strings.TrimSpace(text))
		r.TaintedSpans = append(r.TaintedSpans, tainted...)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if r.SessionID == "" {
		r.SessionID = strings.TrimSuffix(filepath.Base(path), ".jsonl")
	}
	r.Text = b.String()
	r.Chars = len(r.Text)
	return r, nil
}

const taintSpanLimit = 1200

func renderContent(raw json.RawMessage) (string, []string) {
	if len(raw) == 0 {
		return "", nil
	}
	// Content may be a plain string or a block array.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", nil
	}
	var b strings.Builder
	var tainted []string
	for _, blk := range blocks {
		switch blk.Type {
		case "text":
			b.WriteString(blk.Text)
			b.WriteString("\n")
		case "tool_result":
			span := flattenToolResult(blk.Content)
			if span == "" {
				continue
			}
			if len(span) > taintSpanLimit {
				span = span[:taintSpanLimit] + " …[truncated]"
			}
			tainted = append(tainted, span)
			b.WriteString(TaintOpen)
			b.WriteString(span)
			b.WriteString(TaintClose)
			b.WriteString("\n")
		case "tool_use":
			// Tool invocations by the assistant are dialogue-adjacent; note
			// the call without its payload.
			if blk.Name != "" {
				fmt.Fprintf(&b, "[called tool: %s]\n", blk.Name)
			}
		}
	}
	return b.String(), tainted
}

func flattenToolResult(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return string(raw)
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// RenderPlain wraps non-transcript text input (files, stdin) for
// extraction. If untrusted is true the whole body is one tainted span.
func RenderPlain(text string, untrusted bool) *Rendered {
	r := &Rendered{Turns: 1}
	if untrusted {
		r.TaintedSpans = []string{text}
		r.Text = TaintOpen + text + TaintClose
	} else {
		r.Text = text
	}
	r.Chars = len(r.Text)
	return r
}
