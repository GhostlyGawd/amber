package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ghostlygawd/amber/internal/search"
	"github.com/ghostlygawd/amber/internal/store"
	"github.com/ghostlygawd/amber/internal/trust"
)

func cmdForget() *cobra.Command {
	var (
		byQuery  string
		byEntity string
	)
	c := &cobra.Command{
		Use:   "forget [id]",
		Short: "Soft-delete memories (tombstone; reversible with restore)",
		Long: `Tombstone a memory by id, by query, or every memory linked to an entity.
This is reversible soft deletion: tombstones are excluded from recall and
injection, but their rows remain; ` + "`amber restore <id>`" + ` reverses it.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := openEnv()
			if err != nil {
				return err
			}
			defer e.Close()

			var targets []*store.Memory
			switch {
			case len(args) == 1:
				m, err := e.Store.Get(args[0])
				if err != nil {
					return err
				}
				targets = append(targets, m)
			case byEntity != "":
				eid, err := e.Store.FindEntity(byEntity)
				if err != nil {
					return err
				}
				if eid == "" {
					return fmt.Errorf("unknown entity %q", byEntity)
				}
				ids, err := e.Store.MemoryIDsForEntity(eid, []string{store.StatusActive, store.StatusAging, store.StatusQuarantined, store.StatusSuperseded})
				if err != nil {
					return err
				}
				for _, id := range ids {
					if m, err := e.Store.Get(id); err == nil {
						targets = append(targets, m)
					}
				}
			case byQuery != "":
				rs, err := search.Recall(e.Store, e.Embedder, search.Request{Query: byQuery, Limit: 20})
				if err != nil {
					return err
				}
				for _, r := range rs {
					targets = append(targets, r.Memory)
				}
			default:
				return fmt.Errorf("give an id, --query, or --entity")
			}

			if len(targets) == 0 {
				fmt.Println("nothing to forget")
				return nil
			}
			fmt.Printf("will tombstone %d memor%s:\n", len(targets), plural(len(targets), "y", "ies"))
			for _, m := range targets {
				fmt.Println("  " + memLine(m))
			}
			if !confirm("proceed?") {
				return fmt.Errorf("aborted (use --yes to skip the prompt)")
			}
			via := "forget"
			if byEntity != "" {
				via = "forget --entity"
			}
			n := 0
			for _, m := range targets {
				if m.Status == store.StatusTombstoned {
					continue
				}
				if err := e.Store.SetStatus(m.ID, store.StatusTombstoned, store.OpTombstone, map[string]any{"via": via}); err != nil {
					return err
				}
				n++
			}
			e.Store.BumpCounter("forget")
			fmt.Printf("tombstoned %d (reversible: amber restore <id>)\n", n)
			return nil
		},
	}
	c.Flags().StringVar(&byQuery, "query", "", "tombstone top matches for a query (max 20, previewed)")
	c.Flags().StringVar(&byEntity, "entity", "", "tombstone every memory linked to this entity")
	return c
}

func cmdRestore() *cobra.Command {
	c := &cobra.Command{
		Use:   "restore <id>",
		Short: "Reverse a tombstone, quarantine-rejection, or aging demotion",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := openEnv()
			if err != nil {
				return err
			}
			defer e.Close()
			m, err := e.Store.Get(args[0])
			if err != nil {
				return err
			}
			switch m.Status {
			case store.StatusActive:
				fmt.Println("already active")
				return nil
			case store.StatusSuperseded:
				return fmt.Errorf("%s was superseded by %s; forget the newer one instead if it is wrong", shortID(m.ID), shortID(m.SupersededBy))
			}
			target := restoreTargetStatus(m)
			if err := e.Store.SetStatus(m.ID, target, store.OpRestore, map[string]any{"from": m.Status}); err != nil {
				return err
			}
			fmt.Printf("restored %s to %s (was %s)\n", shortID(m.ID), target, m.Status)
			return nil
		},
	}
	return c
}

func restoreTargetStatus(m *store.Memory) string {
	if m.Trust == trust.T3 {
		return store.StatusQuarantined
	}
	return store.StatusActive
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
