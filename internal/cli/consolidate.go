package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/ghostlygawd/amber/internal/config"
	"github.com/ghostlygawd/amber/internal/consolidate"
)

func cmdConsolidate() *cobra.Command {
	var (
		dryRun bool
		since  string
	)
	c := &cobra.Command{
		Use:   "consolidate",
		Short: "Background pass: merge, resolve, absolutize dates, demote, re-index",
		Long: `Merge duplicates, resolve contradictions via supersedence, absolutize
relative dates ("last Tuesday" → a date), demote aged memories, and
re-index.

Never deletes. Every action is journaled to ops and reversible. Runs
opt-in — on demand, or from a scheduler you control (docs/consolidate.md
has cron/launchd snippets). Contrast deliberate: some platforms prune
aggressively enough that guides tell users to back up first; amber
consolidates and keeps everything.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := openEnv()
			if err != nil {
				return err
			}
			defer e.Close()
			var sinceT time.Time
			if since != "" {
				d, err := config.ParseSince(since)
				if err != nil {
					return err
				}
				sinceT = time.Now().Add(-d)
			}
			rep, err := consolidate.Run(e.Store, e.Embedder, sinceT, dryRun)
			if err != nil {
				return err
			}
			if flagFormat == "json" {
				return jsonOut(rep)
			}
			for _, a := range rep.Actions {
				id := a.Memory
				if len(id) > 8 {
					id = id[:8]
				}
				fmt.Printf("%-11s %-9s %s\n", a.Kind, id, a.Detail)
			}
			verb := "consolidated"
			if rep.DryRun {
				verb = "would consolidate"
			}
			fmt.Printf("%s: merged %d, resolved %d, dated %d, demoted %d, re-embedded %d — deleted 0.\n",
				verb, rep.Merged, rep.Resolved, rep.Dated, rep.Demoted, rep.Reindexed)
			if !rep.DryRun {
				e.Store.BumpCounter("consolidate")
			}
			return nil
		},
	}
	c.Flags().BoolVar(&dryRun, "dry-run", false, "plan only; change nothing")
	c.Flags().StringVar(&since, "since", "", "restrict to memories updated within (e.g. 30d)")
	return c
}
