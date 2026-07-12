package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ghostlygawd/amber/internal/scan"
	"github.com/ghostlygawd/amber/internal/trust"
	"github.com/ghostlygawd/amber/internal/writer"
)

func cmdRemember() *cobra.Command {
	var (
		memType    string
		entities   []string
		tags       []string
		importance int
		source     string
		session    string
		force      bool
	)
	c := &cobra.Command{
		Use:   "remember <text>",
		Short: "Store a memory (trust T0, user-stated)",
		Long: `Store a memory. Imperatives are normalized to declarative preferences
(a user instructing themselves is legitimate). Every write runs the
dedupe/supersedence check and the secret/PII scan.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := openEnv()
			if err != nil {
				return err
			}
			defer e.Close()
			text := strings.Join(args, " ")
			out, err := e.Writer.Write(writer.Input{
				Content: text, Type: memType, Importance: importance,
				Trust: trust.T0, Scope: string(e.Scope), Source: orDefault(source, "cli"),
				SessionID: session, Entities: entities, Tags: tags, Force: force,
			})
			if err != nil {
				if errors.Is(err, writer.ErrScanRefused) {
					printScanRefusal(out)
					return &policyError{msg: out.Refusal}
				}
				return err
			}
			e.Store.BumpCounter("remember")
			if flagFormat == "json" {
				return jsonOut(map[string]any{
					"action": out.Action, "id": out.Memory.ID, "content": out.Memory.Content,
					"type": out.Memory.Type, "trust": out.Memory.Trust.String(), "status": out.Memory.Status,
					"normalized": out.Normalized,
					"superseded": memIDOrEmpty(out),
				})
			}
			switch out.Action {
			case "reconfirmed":
				fmt.Printf("reconfirmed %s (already known): %s\n", shortID(out.Memory.ID), oneLine(out.Memory.Content, 90))
			case "superseded":
				fmt.Printf("remembered %s: %s\n", shortID(out.Memory.ID), oneLine(out.Memory.Content, 90))
				fmt.Printf("superseded %s: %s\n", shortID(out.Superseded.ID), oneLine(out.Superseded.Content, 90))
			default:
				verb := "remembered"
				if out.Action == "quarantined" {
					verb = "quarantined"
				}
				fmt.Printf("%s %s [%s %s]: %s\n", verb, shortID(out.Memory.ID), out.Memory.Type, out.Memory.Trust, oneLine(out.Memory.Content, 90))
				if out.Normalized {
					fmt.Println("note: imperative input stored as a declarative preference")
				}
				if out.Ambiguous != nil {
					fmt.Printf("note: kept alongside %s; flagged for review (possible contradiction)\n", shortID(out.Ambiguous.ID))
				}
			}
			if len(out.Findings) > 0 && out.Action != "refused" {
				fmt.Fprintf(os.Stderr, "warning: stored with findings (%s) — review before sharing exports\n", scan.Summary(out.Findings))
			}
			return nil
		},
	}
	c.Flags().StringVar(&memType, "type", "", "fact|preference|decision|event|note (default note; imperatives become preferences)")
	c.Flags().StringArrayVar(&entities, "entity", nil, "link an entity (repeatable)")
	c.Flags().StringSliceVar(&tags, "tags", nil, "comma-separated tags")
	c.Flags().IntVar(&importance, "importance", 3, "1..5")
	c.Flags().StringVar(&source, "source", "", "provenance note (default cli)")
	c.Flags().StringVar(&session, "session", "", "session id")
	c.Flags().BoolVar(&force, "force", false, "store despite scan findings (secrets are redacted)")
	return c
}

func memIDOrEmpty(out *writer.Outcome) string {
	if out.Superseded != nil {
		return out.Superseded.ID
	}
	return ""
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func printScanRefusal(out *writer.Outcome) {
	if out == nil {
		return
	}
	fmt.Fprintln(os.Stderr, "refused: "+out.Refusal)
	for _, f := range out.Findings {
		fmt.Fprintf(os.Stderr, "  %s:%s at %d..%d\n", f.Class, f.Kind, f.Start, f.End)
	}
}
