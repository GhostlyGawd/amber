package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ghostlygawd/amber/internal/store"
	"github.com/ghostlygawd/amber/internal/trust"
)

// reviewItem pairs a memory with why it needs attention.
type reviewItem struct {
	Memory  *store.Memory `json:"memory"`
	Reasons []string      `json:"reasons"`
}

func cmdReview() *cobra.Command {
	var (
		all        bool
		approveIDs []string
		rejectIDs  []string
	)
	c := &cobra.Command{
		Use:   "review",
		Short: "Approval inbox: quarantined and auto-digested memories",
		Long: `Review pending memories: quarantined (T3 untrusted-origin or staged by
review-first posture) and, with --all, active auto-digested (T2) ones.
Approve promotes to T1 user-approved and activates; edit rewrites then
approves; reject tombstones (reversible).

Non-interactive: --approve <id> / --reject <id> (repeatable),
--format json to list.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := openEnv()
			if err != nil {
				return err
			}
			defer e.Close()

			// Non-interactive decisions first.
			if len(approveIDs) > 0 || len(rejectIDs) > 0 {
				for _, id := range approveIDs {
					if err := approve(e, id); err != nil {
						return err
					}
				}
				for _, id := range rejectIDs {
					if err := reject(e, id); err != nil {
						return err
					}
				}
				return nil
			}

			items, err := pendingReview(e, all)
			if err != nil {
				return err
			}
			if flagFormat == "json" {
				return jsonOut(items)
			}
			if len(items) == 0 {
				fmt.Println("review inbox empty")
				return nil
			}
			if !isTTY() || flagYes {
				// Script mode: list, do not decide.
				for _, it := range items {
					fmt.Println(memLine(it.Memory))
					for _, r := range it.Reasons {
						fmt.Println("    " + r)
					}
				}
				fmt.Printf("\n%d pending — approve/reject with --approve <id> / --reject <id>\n", len(items))
				return nil
			}
			return interactiveReview(e, items)
		},
	}
	c.Flags().BoolVar(&all, "all", false, "include active auto-digested (T2) memories awaiting curation")
	c.Flags().StringArrayVar(&approveIDs, "approve", nil, "approve by id (repeatable)")
	c.Flags().StringArrayVar(&rejectIDs, "reject", nil, "reject (tombstone) by id (repeatable)")
	return c
}

func pendingReview(e *env, all bool) ([]reviewItem, error) {
	seen := map[string]*reviewItem{}
	var order []string
	add := func(m *store.Memory, reason string) {
		it, ok := seen[m.ID]
		if !ok {
			it = &reviewItem{Memory: m}
			seen[m.ID] = it
			order = append(order, m.ID)
		}
		it.Reasons = append(it.Reasons, reason)
	}
	quarantined, err := e.Store.List(store.ListFilter{Statuses: []string{store.StatusQuarantined}})
	if err != nil {
		return nil, err
	}
	for _, m := range quarantined {
		add(m, "quarantined ("+m.Trust.Label()+")")
	}
	flags, err := e.Store.OpenFlags()
	if err != nil {
		return nil, err
	}
	for _, f := range flags {
		m, err := e.Store.Get(f.MemoryID)
		if err != nil {
			continue
		}
		if m.Status == store.StatusQuarantined {
			add(m, f.Kind+": "+f.Detail)
			continue
		}
		if !all && f.Kind == store.FlagNeedsReview {
			continue // T2-active curation only with --all
		}
		if m.Status == store.StatusActive || m.Status == store.StatusAging {
			add(m, f.Kind+": "+f.Detail)
		}
	}
	items := make([]reviewItem, 0, len(order))
	for _, id := range order {
		it := seen[id]
		for _, f := range flagsFor(e, id) {
			appendUnique(&it.Reasons, f)
		}
		items = append(items, *it)
	}
	return items, nil
}

func flagsFor(e *env, id string) []string {
	fs, err := e.Store.FlagsFor(id)
	if err != nil {
		return nil
	}
	var out []string
	for _, f := range fs {
		out = append(out, f.Kind+": "+f.Detail)
	}
	return out
}

func appendUnique(list *[]string, s string) {
	for _, x := range *list {
		if x == s {
			return
		}
	}
	*list = append(*list, s)
}

func interactiveReview(e *env, items []reviewItem) error {
	reader := bufio.NewReader(os.Stdin)
	approved, rejected, edited, skipped := 0, 0, 0, 0
	for i, it := range items {
		m := it.Memory
		fmt.Printf("\n[%d/%d] %s\n", i+1, len(items), memLine(m))
		fmt.Printf("       %s · source %s · %s\n", m.Trust.Label(), orDefault(m.Source, "-"), ago(m.CreatedAt))
		for _, r := range it.Reasons {
			fmt.Printf("       %s\n", r)
		}
		fmt.Print("  [a]pprove  [e]dit  [r]eject  [s]kip  [q]uit > ")
		line, _ := reader.ReadString('\n')
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "a":
			if err := approve(e, m.ID); err != nil {
				return err
			}
			approved++
		case "e":
			fmt.Println("  current: " + m.Content)
			fmt.Print("  new    : ")
			newText, _ := reader.ReadString('\n')
			newText = strings.TrimSpace(newText)
			if newText == "" {
				fmt.Println("  empty; skipped")
				skipped++
				continue
			}
			if err := e.Store.UpdateContent(m.ID, newText, "review edit"); err != nil {
				return err
			}
			if e.Embedder != nil {
				if v, err := e.Embedder.Embed(newText); err == nil {
					_ = e.Store.SetEmbedding(m.ID, v)
				}
			}
			if err := approve(e, m.ID); err != nil {
				return err
			}
			edited++
		case "r":
			if err := reject(e, m.ID); err != nil {
				return err
			}
			rejected++
		case "q":
			fmt.Printf("\napproved %d, edited %d, rejected %d, skipped %d, remaining %d\n",
				approved, edited, rejected, skipped, len(items)-i-1)
			return nil
		default:
			skipped++
		}
	}
	fmt.Printf("\napproved %d, edited %d, rejected %d, skipped %d\n", approved, edited, rejected, skipped)
	return nil
}

// approve promotes to T1 user-approved (§9: review promotes to T1),
// activates, raises confidence, and resolves flags.
func approve(e *env, id string) error {
	m, err := e.Store.Get(id)
	if err != nil {
		return err
	}
	if err := e.Store.SetTrust(m.ID, int(trust.T1), store.OpApprove); err != nil {
		return err
	}
	if m.Status == store.StatusQuarantined {
		if err := e.Store.SetStatus(m.ID, store.StatusActive, store.OpApprove, map[string]any{"via": "review"}); err != nil {
			return err
		}
	}
	if err := e.Store.SetConfidence(m.ID, trust.T1.InitialConfidence(), store.OpApprove); err != nil {
		return err
	}
	if err := e.Store.ResolveFlags(m.ID); err != nil {
		return err
	}
	fmt.Printf("approved %s → T1 active\n", shortID(m.ID))
	return nil
}

// reject tombstones (reversible with restore).
func reject(e *env, id string) error {
	m, err := e.Store.Get(id)
	if err != nil {
		return err
	}
	if err := e.Store.SetStatus(m.ID, store.StatusTombstoned, store.OpReject, map[string]any{"via": "review"}); err != nil {
		return err
	}
	if err := e.Store.ResolveFlags(m.ID); err != nil {
		return err
	}
	fmt.Printf("rejected %s (tombstoned; amber restore reverses)\n", shortID(m.ID))
	return nil
}
