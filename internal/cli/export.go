package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ghostlygawd/amber/internal/exporter"
	"github.com/ghostlygawd/amber/internal/store"
)

func cmdExport() *cobra.Command {
	var (
		format string
		all    bool
		out    string
	)
	c := &cobra.Command{
		Use:   "export",
		Short: "Export memories as jsonl, md, or DECISIONS.md",
		Long: `Export is always plain text — never the database. jsonl uses the
published amber.v1 interchange schema (docs/interchange-schema.json).
--format decisions emits an auto-maintained DECISIONS.md.

Outgoing content is scanned; secrets are redacted and a summary printed
before anything is written. Review the output before committing or
sharing.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := openEnv()
			if err != nil {
				return err
			}
			defer e.Close()

			w := os.Stdout
			if out != "" && out != "-" {
				f, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
				if err != nil {
					return err
				}
				defer f.Close()
				w = f
			}

			if format == "decisions" {
				if err := exporter.WriteDecisions(w, e.Store); err != nil {
					return err
				}
				_ = e.Store.AppendOp(store.OpExport, "", map[string]any{"format": format})
				return nil
			}

			ms, err := exporter.Select(e.Store, all)
			if err != nil {
				return err
			}
			rep := exporter.ScanAll(ms)
			fmt.Fprintf(os.Stderr, "scan: %d memories, %d flagged, %d secrets redacted\n",
				rep.MemoriesScanned, rep.MemoriesFlagged, rep.SecretsRedacted)
			for _, f := range rep.Findings {
				fmt.Fprintf(os.Stderr, "  %s\n", f)
			}
			if rep.MemoriesFlagged > 0 {
				fmt.Fprintln(os.Stderr, "review the export before sharing: PII travels with it")
			}

			switch format {
			case "jsonl", "":
				err = exporter.WriteJSONL(w, ms)
			case "md":
				err = exporter.WriteMarkdown(w, ms, e.Dir)
			default:
				return fmt.Errorf("unknown format %q (want jsonl|md|decisions)", format)
			}
			if err != nil {
				return err
			}
			_ = e.Store.AppendOp(store.OpExport, "", map[string]any{"format": format, "count": len(ms), "all": all})
			return nil
		},
	}
	c.Flags().StringVar(&format, "format", "jsonl", "jsonl|md|decisions")
	c.Flags().BoolVar(&all, "all", false, "include superseded, tombstoned, and quarantined")
	c.Flags().StringVarP(&out, "output", "o", "", "write to file instead of stdout")
	return c
}

func cmdImport() *cobra.Command {
	c := &cobra.Command{
		Use:   "import <file.jsonl>",
		Short: "Import an amber.v1 interchange export",
		Long: `Import memories from an amber.v1 JSONL export. Trust tiers, statuses,
and timestamps are preserved; quarantined records stay quarantined.
Records whose content already exists are skipped. Team flow: a teammate
commits an export, you review the diff in the PR, then import.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := openEnv()
			if err != nil {
				return err
			}
			defer e.Close()
			f, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer f.Close()

			res, err := exporter.ImportJSONL(f, e.Store)
			if err != nil {
				return err
			}
			fmt.Printf("read %d, imported %d, skipped %d duplicate%s\n",
				res.Read, res.Imported, res.Skipped, plural(res.Skipped, "", "s"))
			for _, msg := range res.Errors {
				fmt.Fprintln(os.Stderr, "  "+msg)
			}
			if len(res.Errors) > 0 {
				return fmt.Errorf("%d record%s failed", len(res.Errors), plural(len(res.Errors), "", "s"))
			}
			return nil
		},
	}
	return c
}
