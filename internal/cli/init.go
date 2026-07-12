package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ghostlygawd/amber/internal/config"
	"github.com/ghostlygawd/amber/internal/embed"
	"github.com/ghostlygawd/amber/internal/store"
	"github.com/ghostlygawd/amber/internal/transcripts"
	"github.com/ghostlygawd/amber/internal/trust"
	"github.com/ghostlygawd/amber/internal/writer"
)

func cmdInit() *cobra.Command {
	var (
		project  bool
		defaults bool
	)
	c := &cobra.Command{
		Use:   "init",
		Short: "Create a store (global ~/.amber or project ./.amber)",
		Long: `Create the memory store and configuration. Interactive setup offers:
the local embedding model (~30MB download, then fully offline), a
retroactive digest of your local Claude Code transcript history (a
populated store in about ten minutes), and a two-minute seeded interview.

--defaults is fully non-interactive for agent installs: BM25-only floor
(or the local model if already cached), review-first posture, telemetry
off, no prompts.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			interactive := !defaults && isTTY() && !flagYes

			// Resolve target directory.
			var dir string
			if project {
				cwd, err := os.Getwd()
				if err != nil {
					return err
				}
				dir = filepath.Join(cwd, ".amber")
			} else {
				var err error
				dir, err = config.HomeDir()
				if err != nil {
					return err
				}
			}

			fresh := true
			if _, err := os.Stat(filepath.Join(dir, config.DBFileName)); err == nil {
				fresh = false
				fmt.Printf("store already initialized at %s\n", dir)
			}

			s, err := store.Create(dir)
			if err != nil {
				return err
			}
			defer s.Close()
			cfg, err := config.Load(dir)
			if err != nil {
				return err
			}
			if cfg.Store.CreatedAt == "" {
				cfg.Store.CreatedAt = time.Now().UTC().Format(time.RFC3339)
			}

			// Project stores ship a .gitignore covering the DB (§10): the
			// database never lands in version control; exports do.
			if project {
				gi := filepath.Join(dir, ".gitignore")
				if _, err := os.Stat(gi); os.IsNotExist(err) {
					content := "# Amber: the database is private to this machine. Commit exports, not the DB.\n" +
						"amber.db\namber.db-wal\namber.db-shm\n"
					if err := os.WriteFile(gi, []byte(content), 0o644); err != nil {
						return err
					}
				}
			}

			// Embedding setup.
			if fresh || cfg.Embedding.Provider == "none" {
				if err := setupEmbedding(cfg, interactive); err != nil {
					return err
				}
			}

			// Telemetry: one question, counters-only, default No (§10).
			if interactive && fresh {
				fmt.Print("\nKeep local-only usage counters (command counts, never content,\n" +
					"never sent anywhere; v1 has no upload endpoint at all)? [y/N] ")
				var resp string
				fmt.Scanln(&resp)
				cfg.Telemetry.Counters = resp == "y" || resp == "Y"
			}

			if err := config.Save(dir, cfg); err != nil {
				return err
			}
			fmt.Printf("store ready: %s\n", filepath.Join(dir, config.DBFileName))
			fmt.Printf("embedding: %s · digest posture: %s\n", cfg.Embedding.Provider, cfg.Digest.Posture)

			// Retroactive onboarding (D15) + seeded interview.
			if interactive {
				offerRetroactiveDigest(dir)
				offerInterview(s, cfg, project)
			} else {
				fmt.Println("\nnext steps:")
				fmt.Println("  amber remember \"first fact\"     store a memory")
				fmt.Println("  amber digest --transcripts 30d   build a store from your Claude Code history")
				fmt.Println("  amber hooks install              wire into Claude Code sessions")
			}
			return nil
		},
	}
	c.Flags().BoolVar(&project, "project", false, "create ./.amber in this project instead of ~/.amber")
	c.Flags().BoolVar(&defaults, "defaults", false, "non-interactive defaults (for agent installs)")
	return c
}

func setupEmbedding(cfg *config.Config, interactive bool) error {
	modelsDir, err := config.ModelsDir()
	if err != nil {
		return err
	}
	// Cached model wins in every mode: keyless, offline, no questions.
	if embed.ModelCached(modelsDir, embed.DefaultLocalModel) {
		if m2v, err := embed.LoadModel2Vec(modelsDir, embed.DefaultLocalModel); err == nil {
			cfg.Embedding.Provider = "local"
			cfg.Embedding.Model = embed.DefaultLocalModel
			cfg.Embedding.Dims = m2v.Dims()
			fmt.Printf("embedding: local model %s already cached (%d dims)\n", embed.DefaultLocalModel, m2v.Dims())
			return nil
		}
	}
	if !interactive {
		// Agent installs never block on a 30MB download; the BM25 floor
		// works offline and `amber doctor --fetch-model` upgrades later.
		cfg.Embedding.Provider = "none"
		fmt.Println("embedding: BM25-only floor (run `amber doctor --fetch-model` for semantic recall)")
		return nil
	}
	fmt.Println("\nSemantic recall setup — one choice:")
	fmt.Printf("  1) local model (recommended): one ~30MB download, then offline forever, no key\n")
	fmt.Printf("  2) OpenAI-compatible endpoint you configure (opt-in, needs a key or local server)\n")
	fmt.Printf("  3) none: exact/BM25 recall only (upgrade any time)\n")
	fmt.Print("choice [1/2/3, default 1]: ")
	var resp string
	fmt.Scanln(&resp)
	switch strings.TrimSpace(resp) {
	case "", "1":
		fmt.Println("fetching model…")
		if err := embed.FetchModel(modelsDir, embed.DefaultLocalModel, cfg.Embedding.ModelURL, os.Stderr); err != nil {
			fmt.Fprintf(os.Stderr, "download failed (%v); continuing with BM25-only floor\n", err)
			cfg.Embedding.Provider = "none"
			return nil
		}
		m2v, err := embed.LoadModel2Vec(modelsDir, embed.DefaultLocalModel)
		if err != nil {
			fmt.Fprintf(os.Stderr, "model unusable (%v); continuing with BM25-only floor\n", err)
			cfg.Embedding.Provider = "none"
			return nil
		}
		cfg.Embedding.Provider = "local"
		cfg.Embedding.Model = embed.DefaultLocalModel
		cfg.Embedding.Dims = m2v.Dims()
	case "2":
		reader := bufio.NewReader(os.Stdin)
		fmt.Print("endpoint URL (e.g. https://api.openai.com/v1/embeddings): ")
		ep, _ := reader.ReadString('\n')
		fmt.Print("model name: ")
		mn, _ := reader.ReadString('\n')
		fmt.Print("API key env var [OPENAI_API_KEY]: ")
		ke, _ := reader.ReadString('\n')
		cfg.Embedding.Provider = "openai-compat"
		cfg.Embedding.Endpoint = strings.TrimSpace(ep)
		cfg.Embedding.Model = strings.TrimSpace(mn)
		cfg.Embedding.APIKeyEnv = orDefault(strings.TrimSpace(ke), "OPENAI_API_KEY")
	default:
		cfg.Embedding.Provider = "none"
	}
	return nil
}

func offerRetroactiveDigest(storeDir string) {
	dir, err := transcripts.ClaudeDir()
	if err != nil {
		return
	}
	sessions, err := transcripts.Discover(dir, 30*24*time.Hour)
	if err != nil || len(sessions) == 0 {
		return
	}
	fmt.Printf("\nFound %d Claude Code session%s from the last 30 days on this machine.\n",
		len(sessions), plural(len(sessions), "", "s"))
	fmt.Println("Amber can digest them into an initial store — your own history, reviewed")
	fmt.Println("by you, nothing leaves the machine except the digest LLM calls.")
	fmt.Print("digest now? [Y/n] ")
	var resp string
	fmt.Scanln(&resp)
	if resp == "n" || resp == "N" {
		fmt.Println("later: amber digest --transcripts 30d")
		return
	}
	// Route through the command so behavior matches exactly.
	c := cmdDigest()
	c.Flags().Set("transcripts", "30d")
	if err := c.RunE(c, nil); err != nil {
		fmt.Fprintf(os.Stderr, "retroactive digest: %v\n", err)
		fmt.Println("you can retry later: amber digest --transcripts 30d")
	}
}

// offerInterview runs the 2-minute seeded interview: each answer becomes
// a T0 memory through the standard write path.
func offerInterview(s *store.Store, cfg *config.Config, project bool) {
	fmt.Print("\nTwo-minute seeded interview to prime the store? [Y/n] ")
	var resp string
	fmt.Scanln(&resp)
	if resp == "n" || resp == "N" {
		return
	}
	scope := "global"
	if project {
		scope = "project"
	}
	e, _ := embed.New(cfg)
	w := &writer.Writer{Store: s, Config: cfg, Embedder: e}
	reader := bufio.NewReader(os.Stdin)
	questions := []struct{ q, typ, prefix string }{
		{"What should agents call you, and what's your role?", "fact", "User"},
		{"Main languages and frameworks you work in?", "fact", "User works mainly in"},
		{"Editor/tooling preferences agents should respect?", "preference", "User prefers"},
		{"Current main project and what it is?", "fact", "Current project:"},
		{"Coding conventions you always want followed?", "preference", "Convention:"},
		{"Anything agents keep getting wrong that you're tired of repeating?", "preference", "Standing correction:"},
	}
	stored := 0
	for _, item := range questions {
		fmt.Printf("  %s\n  > ", item.q)
		ans, _ := reader.ReadString('\n')
		ans = strings.TrimSpace(ans)
		if ans == "" || strings.EqualFold(ans, "skip") {
			continue
		}
		content := item.prefix + " " + ans
		if strings.HasSuffix(item.prefix, ":") {
			content = item.prefix + " " + ans
		}
		out, err := w.Write(writer.Input{
			Content: content, Type: item.typ, Trust: trust.T0,
			Importance: 4, Scope: scope, Source: "interview",
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "    not stored: %v\n", err)
			continue
		}
		if out.Memory != nil {
			stored++
		}
	}
	fmt.Printf("interview done — %d memor%s stored.\n", stored, plural(stored, "y", "ies"))
}
