package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/ghostlygawd/amber/internal/contextfmt"
	"github.com/ghostlygawd/amber/internal/extract"
	"github.com/ghostlygawd/amber/internal/search"
	"github.com/ghostlygawd/amber/internal/store"
	"github.com/ghostlygawd/amber/internal/transcripts"
	"github.com/ghostlygawd/amber/internal/trust"
)

// cmdHook implements the hidden hook entrypoints that Claude Code
// invokes. They read the hook JSON on stdin per the hooks contract and
// must never fail loudly — a broken memory layer must not break the
// user's session.
func cmdHook() *cobra.Command {
	root := &cobra.Command{
		Use:    "hook",
		Hidden: true,
		Short:  "Internal: Claude Code hook entrypoints",
	}
	root.AddCommand(cmdHookSessionStart(), cmdHookSessionEnd())
	return root
}

type hookInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	CWD            string `json:"cwd"`
	Reason         string `json:"reason"`
	Source         string `json:"source"`
}

func readHookInput() hookInput {
	var in hookInput
	if !isTTY() {
		b, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
		if err == nil && len(b) > 0 {
			_ = json.Unmarshal(b, &in)
		}
	}
	return in
}

// cmdHookSessionStart prints a budgeted briefing to stdout — Claude Code
// adds SessionStart hook stdout to context. T0-T2 active only, deduped
// against CLAUDE.md, hard token budget (§11).
func cmdHookSessionStart() *cobra.Command {
	return &cobra.Command{
		Use:  "session-start",
		RunE: func(cmd *cobra.Command, args []string) error {
			in := readHookInput()
			if in.CWD != "" {
				_ = os.Chdir(in.CWD)
			}
			e, err := openEnv()
			if err != nil {
				return nil // no store: stay silent, never break the session
			}
			defer e.Close()

			results, err := search.Briefing(e.Store, time.Now().UTC(), e.Config.Inject.MaxItems*3)
			if err != nil || len(results) == 0 {
				return nil
			}
			block := contextfmt.Render(results, contextfmt.Options{
				BudgetTokens:  e.Config.Inject.BudgetTokens,
				MaxItems:      e.Config.Inject.MaxItems,
				DedupeAgainst: readInstructionFiles(in.CWD),
				AsOfDates:     true,
			})
			if block.Text == "" {
				return nil
			}
			fmt.Println(block.Text)
			_ = e.Store.AppendOp(store.OpInject, "", map[string]any{
				"tokens": block.Tokens, "items": len(block.Included), "skipped": block.Skipped,
				"session": in.SessionID, "kind": "session-start",
			})
			return nil
		},
	}
}

// readInstructionFiles gathers CLAUDE.md content for dedupe so hooks
// don't double-inject what instruction files already say.
func readInstructionFiles(cwd string) string {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	var out []byte
	for _, p := range []string{
		filepath.Join(cwd, "CLAUDE.md"),
		filepath.Join(cwd, "AGENTS.md"),
	} {
		if b, err := os.ReadFile(p); err == nil && len(b) < 256*1024 {
			out = append(out, b...)
			out = append(out, '\n')
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if b, err := os.ReadFile(filepath.Join(home, ".claude", "CLAUDE.md")); err == nil && len(b) < 256*1024 {
			out = append(out, b...)
		}
	}
	return string(out)
}

// cmdHookSessionEnd digests the finished session transcript. Quiet by
// design: results go to the store (and the review inbox per posture),
// diagnostics to a small log file, nothing to the console.
func cmdHookSessionEnd() *cobra.Command {
	return &cobra.Command{
		Use:  "session-end",
		RunE: func(cmd *cobra.Command, args []string) error {
			in := readHookInput()
			if in.TranscriptPath == "" {
				return nil
			}
			if in.CWD != "" {
				_ = os.Chdir(in.CWD)
			}
			e, err := openEnv()
			if err != nil {
				return nil
			}
			defer e.Close()

			r, err := transcripts.Parse(in.TranscriptPath)
			if err != nil {
				logHook("session-end: parse %s: %v", in.TranscriptPath, err)
				return nil
			}
			if r.Chars < e.Config.Digest.MinTranscriptChars {
				return nil // below threshold; not worth an LLM call
			}
			backend, err := extract.NewBackend(e.Config)
			if err != nil {
				logHook("session-end: %v", err)
				return nil
			}
			opts := extract.Options{
				SessionID:       orDefault(in.SessionID, r.SessionID),
				Source:          "digest:session-end",
				Scope:           string(e.Scope),
				BaseTrust:       trust.T2,
				ReviewFirst:     e.Config.Digest.Posture == "review-first",
				ImportanceFloor: e.Config.Digest.ImportanceFloor,
			}
			res, err := extract.Run(cmd.Context(), backend, r, opts)
			if err != nil {
				logHook("session-end: extract: %v", err)
				return nil
			}
			if err := extract.Apply(e.Writer, res, opts); err != nil {
				logHook("session-end: apply: %v", err)
				return nil
			}
			logHook("session %s: %s", opts.SessionID, digestSummary(res))
			return nil
		},
	}
}

// logHook appends to ~/.amber/logs/hooks.log, truncating at 1MB.
func logHook(format string, args ...any) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	if h := os.Getenv("AMBER_HOME"); h != "" {
		home = h
	} else {
		home = filepath.Join(home, ".amber")
	}
	dir := filepath.Join(home, "logs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	path := filepath.Join(dir, "hooks.log")
	if fi, err := os.Stat(path); err == nil && fi.Size() > 1<<20 {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s "+format+"\n", append([]any{time.Now().Format(time.RFC3339)}, args...)...)
}
