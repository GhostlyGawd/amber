package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ghostlygawd/amber/internal/tui"
)

func cmdBrowse() *cobra.Command {
	return &cobra.Command{
		Use:   "browse",
		Short: "TUI: search, list, filter, and inspect the store",
		Long: `Terminal browser for the store: live hybrid search (/), status filters
(tab through active/aging/quarantined/superseded/tombstoned), full-record
inspection with supersedence chains and trust tiers, and inline
approve/tombstone for quarantined items.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !isTTY() {
				return fmt.Errorf("browse needs a terminal; use `amber recall --format json` in scripts")
			}
			e, err := openEnv()
			if err != nil {
				return err
			}
			defer e.Close()
			e.Store.BumpCounter("browse")
			return tui.Run(e.Store, e.Embedder)
		},
	}
}
