package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ghostlygawd/amber/internal/config"
	"github.com/ghostlygawd/amber/internal/contextfmt"
	"github.com/ghostlygawd/amber/internal/embed"
	"github.com/ghostlygawd/amber/internal/search"
	"github.com/ghostlygawd/amber/internal/store"
)

func cmdRecall() *cobra.Command {
	var (
		limit   int
		entity  string
		memType string
		since   string
		why     bool
		history bool
	)
	c := &cobra.Command{
		Use:   "recall <query>",
		Short: "Hybrid search over memories",
		Long: `Hybrid semantic + exact recall with reciprocal rank fusion, modulated
by importance, trust, and per-type recency.

--format context emits a token-budgeted block framed as data, not
instructions, containing only active T0-T2 memories. --why explains every
hit: which candidate lists it appeared on, scores, and multipliers.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.Join(args, " ")
			var sinceT time.Time
			if since != "" {
				d, err := config.ParseSince(since)
				if err != nil {
					return err
				}
				sinceT = time.Now().Add(-d)
			}
			var types []string
			if memType != "" {
				if !store.ValidType(memType) {
					return fmt.Errorf("unknown type %q", memType)
				}
				types = []string{memType}
			}
			req := search.Request{Query: query, Limit: limit, Entity: entity, Types: types, Since: sinceT, History: history}

			results, err := runScopedRecall(req)
			if err != nil {
				return err
			}

			switch flagFormat {
			case "json":
				return jsonOut(toResultJSON(results, why))
			case "context":
				e, err := openEnv()
				if err != nil {
					return err
				}
				defer e.Close()
				block := contextfmt.Render(results, contextfmt.Options{
					BudgetTokens: e.Config.Inject.BudgetTokens,
					MaxItems:     e.Config.Inject.MaxItems,
					AsOfDates:    true,
				})
				if block.Text != "" {
					fmt.Println(block.Text)
				}
				_ = e.Store.AppendOp(store.OpInject, "", map[string]any{
					"tokens": block.Tokens, "items": len(block.Included), "skipped": block.Skipped, "query": query,
				})
				return nil
			default:
				printResults(os.Stdout, results, why)
				return nil
			}
		},
	}
	c.Flags().IntVar(&limit, "limit", 0, "max results (default from config, 8)")
	c.Flags().StringVar(&entity, "entity", "", "filter by entity name or alias")
	c.Flags().StringVar(&memType, "type", "", "filter by memory type")
	c.Flags().StringVar(&since, "since", "", "only memories updated within (e.g. 30d, 2w, 12h)")
	c.Flags().BoolVar(&why, "why", false, "show retrieval attribution per hit")
	c.Flags().BoolVar(&history, "history", false, "include superseded, tombstoned, and quarantined memories")
	return c
}

// runScopedRecall handles --scope all by querying both stores and fusing.
func runScopedRecall(req search.Request) ([]search.Result, error) {
	if config.Scope(flagScope) != config.ScopeAll {
		e, err := openEnv()
		if err != nil {
			return nil, err
		}
		defer e.Close()
		if req.Limit <= 0 {
			req.Limit = e.Config.Recall.Limit
		}
		e.Store.BumpCounter("recall")
		return search.Recall(e.Store, e.Embedder, req)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	var lists [][]search.Result
	for _, sc := range []config.Scope{config.ScopeProject, config.ScopeGlobal} {
		dir, _, err := config.ResolveStoreDir(sc, cwd)
		if err != nil {
			continue // e.g. no project store
		}
		s, err := store.Open(dir)
		if err != nil {
			continue
		}
		cfg, err := config.Load(dir)
		if err != nil {
			s.Close()
			continue
		}
		em, err := embed.New(cfg)
		if err != nil {
			em = nil
		}
		r := req
		if r.Limit <= 0 {
			r.Limit = cfg.Recall.Limit
		}
		rs, err := search.Recall(s, em, r)
		s.Close()
		if err != nil {
			return nil, err
		}
		lists = append(lists, rs)
	}
	merged := search.MergeScopes(lists...)
	if req.Limit > 0 && len(merged) > req.Limit {
		merged = merged[:req.Limit]
	}
	return merged, nil
}
