package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ghostlygawd/amber/internal/belief"
	"github.com/ghostlygawd/amber/internal/store"
)

func cmdShow() *cobra.Command {
	c := &cobra.Command{
		Use:   "show <id|entity>",
		Short: "Full record, or an entity dossier",
		Long: `Show one memory in full — provenance, trust, confidence, supersedence
chain, journal history — or, given an entity name, a dossier: linked
memories, timeline, and aliases.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := openEnv()
			if err != nil {
				return err
			}
			defer e.Close()
			arg := args[0]

			m, err := e.Store.Get(arg)
			if err == nil {
				return showMemory(e, m)
			}
			if !errors.Is(err, store.ErrNotFound) && !errors.Is(err, store.ErrAmbiguousID) {
				return err
			}
			// Fall back to entity dossier.
			eid, eerr := e.Store.FindEntity(arg)
			if eerr != nil {
				return eerr
			}
			if eid == "" {
				return fmt.Errorf("no memory or entity matches %q", arg)
			}
			return showEntityDossier(e, eid)
		},
	}
	return c
}

func showMemory(e *env, m *store.Memory) error {
	older, newer, _ := e.Store.Chain(m.ID)
	ops, _ := e.Store.OpsFor(m.ID)
	flags, _ := e.Store.FlagsFor(m.ID)
	effConf := belief.EffectiveConfidence(m, time.Now().UTC())

	if flagFormat == "json" {
		return jsonOut(map[string]any{
			"memory": m, "trust_label": m.Trust.Label(), "effective_confidence": effConf,
			"chain_older": older, "chain_newer": newer, "ops": ops, "open_flags": flags,
		})
	}

	fmt.Printf("id          %s\n", m.ID)
	fmt.Printf("content     %s\n", m.Content)
	fmt.Printf("type        %s\n", m.Type)
	fmt.Printf("status      %s%s\n", m.Status, supersededSuffix(m))
	fmt.Printf("trust       %s (%s)\n", m.Trust, m.Trust.Label())
	fmt.Printf("importance  %d/5\n", m.Importance)
	fmt.Printf("confidence  %.2f stored, %.2f effective\n", m.Confidence, effConf)
	fmt.Printf("scope       %s\n", m.Scope)
	fmt.Printf("source      %s\n", orDefault(m.Source, "-"))
	if m.SessionID != "" {
		fmt.Printf("session     %s\n", m.SessionID)
	}
	fmt.Printf("created     %s\n", m.CreatedAt.Format(time.RFC3339))
	fmt.Printf("updated     %s\n", m.UpdatedAt.Format(time.RFC3339))
	fmt.Printf("confirmed   %s\n", m.LastConfirmedAt.Format(time.RFC3339))
	if len(m.Entities) > 0 {
		var names []string
		for _, en := range m.Entities {
			names = append(names, fmt.Sprintf("%s (%s)", en.Name, en.Type))
		}
		fmt.Printf("entities    %s\n", strings.Join(names, ", "))
	}
	if len(m.Tags) > 0 {
		fmt.Printf("tags        %s\n", strings.Join(m.Tags, ", "))
	}
	if len(flags) > 0 {
		for _, f := range flags {
			fmt.Printf("flag        %s: %s\n", f.Kind, f.Detail)
		}
	}
	if len(older) > 0 || len(newer) > 0 {
		fmt.Println("chain:")
		for i := len(older) - 1; i >= 0; i-- {
			fmt.Printf("  ← %s\n", memLine(older[i]))
		}
		fmt.Printf("  ● %s (this)\n", shortID(m.ID))
		for _, n := range newer {
			fmt.Printf("  → %s\n", memLine(n))
		}
	}
	if len(ops) > 0 {
		fmt.Println("journal:")
		for _, o := range ops {
			fmt.Printf("  %s  %-11s %s\n", o.TS.Format("2006-01-02 15:04"), o.Op, compactJSON(o.Payload))
		}
	}
	return nil
}

func supersededSuffix(m *store.Memory) string {
	if m.SupersededBy != "" {
		return " by " + shortID(m.SupersededBy)
	}
	return ""
}

func compactJSON(raw []byte) string {
	s := string(raw)
	if len(s) > 120 {
		s = s[:119] + "…"
	}
	return s
}

func showEntityDossier(e *env, entityID string) error {
	ents, err := e.Store.ListEntities("")
	if err != nil {
		return err
	}
	var ent *store.Entity
	for i := range ents {
		if ents[i].ID == entityID {
			ent = &ents[i]
			break
		}
	}
	if ent == nil {
		return fmt.Errorf("entity vanished")
	}
	ids, err := e.Store.MemoryIDsForEntity(entityID, nil)
	if err != nil {
		return err
	}
	var mems []*store.Memory
	for _, id := range ids {
		if m, err := e.Store.Get(id); err == nil {
			mems = append(mems, m)
		}
	}
	store.SortMemoriesByUpdated(mems)

	if flagFormat == "json" {
		return jsonOut(map[string]any{"entity": ent, "memories": mems})
	}
	fmt.Printf("entity      %s (%s)\n", ent.Name, ent.Type)
	if len(ent.Aliases) > 0 {
		fmt.Printf("aliases     %s\n", strings.Join(ent.Aliases, ", "))
	}
	fmt.Printf("memories    %d\n", len(mems))
	fmt.Println("timeline:")
	for _, m := range mems {
		fmt.Printf("  %s  %s\n", m.UpdatedAt.Format("2006-01-02"), memLine(m))
	}
	return nil
}
