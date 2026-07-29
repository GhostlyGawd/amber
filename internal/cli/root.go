// Package cli implements the amber command surface (§5).
//
// Conventions: every command supports --format json where output exists;
// every interactive step has a non-interactive flag; exit codes are
// script-safe (0 ok, 1 error, 2 refused-by-policy). Voice: man-page
// precision — declarative sentences, numbers, no exclamation points.
package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ghostlygawd/amber/internal/config"
	"github.com/ghostlygawd/amber/internal/embed"
	"github.com/ghostlygawd/amber/internal/store"
	"github.com/ghostlygawd/amber/internal/version"
	"github.com/ghostlygawd/amber/internal/writer"
)

// ExitPolicy is the exit code for policy refusals (scan block, etc.).
const ExitPolicy = 2

var (
	flagScope  string
	flagFormat string
	flagYes    bool
)

// Root builds the command tree.
func Root() *cobra.Command {
	root := &cobra.Command{
		Use:   "amber",
		Short: "Local-first, long-term memory for AI coding agents",
		Long: `amber — local-first, long-term memory for AI coding agents.

One SQLite file you own. No Docker, no API key, no account, offline by
default. Instructions are what you tell an agent (CLAUDE.md / AGENTS.md);
memory is what it learns (amber).

Every memory is inspectable, every write previewable, every change
reversible, every untrusted input quarantined.`,
		Version:       fmt.Sprintf("%s (commit %s, built %s)", version.Version, version.Commit, version.Date),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&flagScope, "scope", "", "store scope: global|project (default: project if present, else global)")
	root.PersistentFlags().StringVar(&flagFormat, "format", "", "output format (text|json; commands may add more)")
	root.PersistentFlags().BoolVar(&flagYes, "yes", false, "assume yes; never prompt (for scripts and agents)")

	root.AddCommand(
		cmdInit(), cmdRemember(), cmdRecall(), cmdShow(), cmdForget(), cmdRestore(),
		cmdEntities(), cmdDigest(), cmdReview(), cmdConsolidate(), cmdBrowse(),
		cmdServe(), cmdExport(), cmdImport(), cmdHooks(), cmdHook(),
		cmdStatus(), cmdConfig(), cmdDoctor(),
	)
	return root
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	err := Root().Execute()
	if err == nil {
		return 0
	}
	var pe *policyError
	if errors.As(err, &pe) {
		fmt.Fprintln(os.Stderr, "amber:", pe.msg)
		return ExitPolicy
	}
	fmt.Fprintln(os.Stderr, "amber:", err)
	return 1
}

type policyError struct{ msg string }

func (e *policyError) Error() string { return e.msg }

// env binds a command invocation to a resolved store + config + embedder.
type env struct {
	Dir               string
	Scope             config.Scope
	Store             *store.Store
	Config            *config.Config
	Embedder          embed.Embedder
	MigrationEmbedder embed.Embedder
	Writer            *writer.Writer
}

// openEnv resolves scope and opens the store. Commands that mutate or
// read the store use this; init uses its own path.
func openEnv() (*env, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	dir, scope, err := config.ResolveStoreDir(config.Scope(flagScope), cwd)
	if err != nil {
		return nil, err
	}
	s, err := store.Open(dir)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(dir)
	if err != nil {
		s.Close()
		return nil, err
	}
	e, err := embed.New(cfg)
	configuredEmbedder := e
	if err != nil {
		// A broken embedder config must not brick the store: degrade to
		// the BM25 floor with a warning on stderr.
		fmt.Fprintf(os.Stderr, "amber: embedding provider unavailable (%v); falling back to lexical search\n", err)
		e = nil
	}
	// Mixed-embedding-model stores are refused (§6): comparing vectors
	// from different models is meaningless. Degrade to lexical and point
	// at the migration.
	if e != nil {
		if pinned, _ := s.GetMeta(store.MetaEmbeddingModel); pinned != "" && pinned != e.Name() {
			fmt.Fprintf(os.Stderr, "amber: store vectors were built with %s but config selects %s;\n"+
				"       semantic search disabled until `amber doctor --reembed`\n", pinned, e.Name())
			e = nil
		}
	}
	return &env{
		Dir: dir, Scope: scope, Store: s, Config: cfg, Embedder: e,
		MigrationEmbedder: configuredEmbedder,
		Writer:            &writer.Writer{Store: s, Config: cfg, Embedder: e},
	}, nil
}

func (e *env) Close() { e.Store.Close() }

// jsonOut marshals v as indented JSON to stdout.
func jsonOut(v any) error {
	enc := newJSONEncoder(os.Stdout)
	return enc.Encode(v)
}

// confirm prompts y/N on the tty. --yes short-circuits. Non-tty without
// --yes returns false (never hang a pipeline).
func confirm(prompt string) bool {
	if flagYes {
		return true
	}
	if !isTTY() {
		return false
	}
	fmt.Printf("%s [y/N] ", prompt)
	var resp string
	fmt.Scanln(&resp)
	return resp == "y" || resp == "Y" || resp == "yes"
}

func isTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
