package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ghostlygawd/amber/internal/config"
	"github.com/ghostlygawd/amber/internal/extract"
	"github.com/ghostlygawd/amber/internal/store"
	"github.com/ghostlygawd/amber/internal/transcripts"
	"github.com/ghostlygawd/amber/internal/trust"
)

func cmdDigest() *cobra.Command {
	var (
		session     string
		dryRun      bool
		fromHistory string
		untrusted   bool
	)
	c := &cobra.Command{
		Use:   "digest [FILE]",
		Short: "LLM extraction from a transcript or memory file",
		Long: `Extract durable memories from a transcript (file or stdin), a memory
file (MEMORY.md, CLAUDE.md, AGENTS.md, platform exports — the migration
path), or your local Claude Code history (--transcripts 30d, the
retroactive-onboarding path).

Extraction is taint-aware and declarative-only: content from tool/web
output is quarantined (T3); instruction-shaped candidates are quarantined
regardless of origin. Diff preview by default; --yes applies.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := openEnv()
			if err != nil {
				return err
			}
			defer e.Close()

			if fromHistory != "" {
				d, err := config.ParseSince(fromHistory)
				if err != nil {
					return err
				}
				return digestHistory(e, d, dryRun)
			}

			var rendered *transcripts.Rendered
			var sourceName string
			switch {
			case len(args) == 1:
				b, err := os.ReadFile(args[0])
				if err != nil {
					return err
				}
				sourceName = filepath.Base(args[0])
				rendered = renderInput(string(b), args[0], untrusted)
			default:
				if isTTY() {
					return fmt.Errorf("give a file, pipe a transcript on stdin, or use --transcripts 30d")
				}
				b, err := io.ReadAll(os.Stdin)
				if err != nil {
					return err
				}
				sourceName = "stdin"
				rendered = renderInput(string(b), "", untrusted)
			}

			opts := extract.Options{
				SessionID:   session,
				Source:      "digest:" + sourceName,
				Scope:       string(e.Scope),
				BaseTrust:   baseTrustFor(sourceName),
				ReviewFirst: e.Config.Digest.Posture == "review-first",
			}
			return runDigest(e, rendered, opts, dryRun)
		},
	}
	c.Flags().StringVar(&session, "session", "", "session id recorded on extracted memories")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "preview only; write nothing")
	c.Flags().StringVar(&fromHistory, "transcripts", "", "digest local Claude Code transcripts from the last window (e.g. 30d)")
	c.Flags().BoolVar(&untrusted, "untrusted", false, "treat the entire input as untrusted origin (quarantine all candidates)")
	return c
}

// renderInput picks taint posture by input kind: raw transcripts with
// tool output already carry markers; a plain memory file is clean unless
// --untrusted.
func renderInput(text, path string, untrusted bool) *transcripts.Rendered {
	if strings.Contains(text, transcripts.TaintOpen) {
		r := transcripts.RenderPlain(text, false)
		// Recover spans between existing markers for the post-screen.
		parts := strings.Split(text, transcripts.TaintOpen)
		for _, p := range parts[1:] {
			if i := strings.Index(p, transcripts.TaintClose); i > 0 {
				r.TaintedSpans = append(r.TaintedSpans, p[:i])
			}
		}
		return r
	}
	if strings.HasSuffix(path, ".jsonl") {
		if r, err := transcripts.Parse(path); err == nil {
			return r
		}
	}
	return transcripts.RenderPlain(text, untrusted)
}

// baseTrustFor: user-authored instruction files digest at T1 (the user
// wrote them); everything else at T2 (auto-digest).
func baseTrustFor(name string) trust.Tier {
	base := strings.ToUpper(filepath.Base(name))
	if base == "CLAUDE.MD" || base == "AGENTS.MD" {
		return trust.T1
	}
	return trust.T2
}

// runDigest is shared by digest/init/hook flows: extract, preview, apply.
func runDigest(e *env, rendered *transcripts.Rendered, opts extract.Options, dryRun bool) error {
	backend, err := extract.NewBackend(e.Config)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "extracting with %s (%d chars, %d turns)…\n", backend.Name(), rendered.Chars, rendered.Turns)
	res, err := extract.Run(context.Background(), backend, rendered, opts)
	if err != nil {
		return err
	}
	printDigestPreview(res)
	if dryRun {
		fmt.Println("\n(dry run; nothing written)")
		return nil
	}
	storeCount := 0
	for _, c := range res.Candidates {
		if c.Disposition != "drop" {
			storeCount++
		}
	}
	if storeCount == 0 {
		fmt.Println("nothing to store")
		return nil
	}
	if !confirm(fmt.Sprintf("apply %d change%s?", storeCount, plural(storeCount, "", "s"))) {
		return fmt.Errorf("aborted (use --yes to apply without prompting)")
	}
	if err := extract.Apply(e.Writer, res, opts); err != nil {
		return err
	}
	e.Store.BumpCounter("digest")
	fmt.Println(digestSummary(res))
	return nil
}

// printDigestPreview renders the diff-style preview (§5: every write
// previewable).
func printDigestPreview(res *extract.Result) {
	if len(res.Candidates) == 0 {
		fmt.Println("no durable memories found")
		return
	}
	for _, c := range res.Candidates {
		switch c.Disposition {
		case "store":
			fmt.Printf("+ [%s i%d] %s\n", c.Type, c.Importance, oneLine(c.Content, 110))
			if c.SupersedesHint != "" {
				fmt.Printf("  ~ supersedes: %s\n", oneLine(c.SupersedesHint, 100))
			}
		case "quarantine":
			fmt.Printf("⛔ [%s → review inbox] %s\n", c.Type, oneLine(c.Content, 100))
			fmt.Printf("   reason: %s\n", c.Reason)
		case "drop":
			fmt.Printf("- dropped (%s): %s\n", c.Reason, oneLine(c.Content, 80))
		}
	}
}

// digestSummary is the calm one-liner (§30 voice: wit lives only in tool
// output, where it earns trust).
func digestSummary(res *extract.Result) string {
	parts := []string{fmt.Sprintf("learned %d", len(res.Stored))}
	if res.Superseded > 0 {
		parts = append(parts, fmt.Sprintf("updated %d", res.Superseded))
	}
	if res.Reconfirmed > 0 {
		parts = append(parts, fmt.Sprintf("reconfirmed %d", res.Reconfirmed))
	}
	if n := len(res.Quarantined); n > 0 {
		parts = append(parts, fmt.Sprintf("quarantined %d (pending review)", n))
	}
	if res.Dropped > 0 {
		parts = append(parts, fmt.Sprintf("dropped %d", res.Dropped))
	}
	s := strings.Join(parts, ", ")
	if res.Ignored > 0 {
		s += ", ignored the small talk"
	}
	return s + "."
}

// digestHistory implements retroactive onboarding (D15): digest local
// Claude Code transcripts from the last window.
func digestHistory(e *env, within time.Duration, dryRun bool) error {
	dir, err := transcripts.ClaudeDir()
	if err != nil {
		return err
	}
	sessions, err := transcripts.Discover(dir, within)
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		fmt.Printf("no Claude Code transcripts found under %s\n", filepath.Join(dir, "projects"))
		return nil
	}
	fmt.Printf("found %d session%s across your local history\n", len(sessions), plural(len(sessions), "", "s"))
	minChars := e.Config.Digest.MinTranscriptChars
	done := 0
	for _, sess := range sessions {
		r, err := transcripts.Parse(sess.Path)
		if err != nil || r.Chars < minChars {
			continue
		}
		fmt.Printf("\n― session %s (%s, %d chars)\n", shortID(sess.SessionID), sess.ModTime.Format("2006-01-02"), r.Chars)
		opts := extract.Options{
			SessionID:   sess.SessionID,
			Source:      "digest:history:" + sess.Project,
			Scope:       string(e.Scope),
			BaseTrust:   trust.T2,
			ReviewFirst: e.Config.Digest.Posture == "review-first",
		}
		if err := runDigest(e, r, opts, dryRun); err != nil {
			fmt.Fprintf(os.Stderr, "  skipped: %v\n", err)
			continue
		}
		done++
	}
	if done > 0 && !dryRun {
		fmt.Printf("\ndigested %d session%s — review the inbox: amber review\n", done, plural(done, "", "s"))
		_ = e.Store.AppendOp("onboard", "", map[string]any{"sessions": done})
	}
	return nil
}

var _ = store.StatusActive // keep import if summary helpers move
