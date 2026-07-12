package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/ghostlygawd/amber/internal/store"
	"github.com/ghostlygawd/amber/internal/version"
)

func cmdStatus() *cobra.Command {
	c := &cobra.Command{
		Use:   "status",
		Short: "Store stats, injection-budget usage, pending review count",
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := openEnv()
			if err != nil {
				return err
			}
			defer e.Close()

			byStatus, _ := e.Store.CountByStatus()
			byType, _ := e.Store.CountBy("type")
			byTrust, _ := e.Store.CountBy("trust")
			ents, _ := e.Store.ListEntities("")
			pending, _ := pendingReview(e, false)
			pendingCount := len(pending)
			modelID, _ := e.Store.GetMeta(store.MetaEmbeddingModel)

			// Injection budget observability: last SessionStart injections.
			injOps, _ := e.Store.RecentOps(5, store.OpInject)
			type inj struct {
				TS     string `json:"ts"`
				Tokens int    `json:"tokens"`
				Items  int    `json:"items"`
			}
			var injections []inj
			for _, o := range injOps {
				var p struct {
					Tokens int `json:"tokens"`
					Items  int `json:"items"`
				}
				_ = json.Unmarshal(o.Payload, &p)
				injections = append(injections, inj{TS: o.TS.Format(time.RFC3339), Tokens: p.Tokens, Items: p.Items})
			}

			embName := "none (BM25-only lexical floor)"
			if e.Embedder != nil {
				embName = fmt.Sprintf("%s (%d dims)", e.Embedder.Name(), e.Embedder.Dims())
			}

			if flagFormat == "json" {
				return jsonOut(map[string]any{
					"version": version.Version, "store": e.Dir, "scope": e.Scope,
					"counts":   map[string]any{"status": byStatus, "type": byType, "trust": byTrust},
					"entities": len(ents), "pending_review": pendingCount,
					"embedding": map[string]any{"provider": e.Config.Embedding.Provider, "active": embName, "store_model": modelID},
					"digest":    map[string]any{"backend": e.Config.Digest.Backend, "posture": e.Config.Digest.Posture},
					"inject":    map[string]any{"budget_tokens": e.Config.Inject.BudgetTokens, "recent": injections},
					"telemetry": e.Config.Telemetry.Counters,
				})
			}

			total := 0
			for _, n := range byStatus {
				total += n
			}
			fmt.Printf("store       %s (%s scope)\n", e.Dir, e.Scope)
			fmt.Printf("memories    %d total — %d active, %d aging, %d superseded, %d quarantined, %d tombstoned\n",
				total, byStatus[store.StatusActive], byStatus[store.StatusAging],
				byStatus[store.StatusSuperseded], byStatus[store.StatusQuarantined], byStatus[store.StatusTombstoned])
			fmt.Printf("types       fact %d · preference %d · decision %d · event %d · note %d\n",
				byType["fact"], byType["preference"], byType["decision"], byType["event"], byType["note"])
			fmt.Printf("trust       T0 %d · T1 %d · T2 %d · T3 %d\n", byTrust["0"], byTrust["1"], byTrust["2"], byTrust["3"])
			fmt.Printf("entities    %d\n", len(ents))
			fmt.Printf("embedding   %s\n", embName)
			fmt.Printf("digest      backend %s, posture %s\n", e.Config.Digest.Backend, e.Config.Digest.Posture)
			if pendingCount > 0 {
				fmt.Printf("review      %d item%s pending — amber review\n", pendingCount, plural(pendingCount, "", "s"))
			} else {
				fmt.Printf("review      inbox empty\n")
			}
			if len(injections) > 0 {
				fmt.Printf("injection   budget %d tokens; last: %d tokens / %d items\n",
					e.Config.Inject.BudgetTokens, injections[0].Tokens, injections[0].Items)
			} else {
				fmt.Printf("injection   budget %d tokens; no injections recorded yet\n", e.Config.Inject.BudgetTokens)
			}
			fmt.Printf("telemetry   %s\n", offOn(e.Config.Telemetry.Counters))
			maybeSuggestAutoPosture(e)
			return nil
		},
	}
	return c
}

// maybeSuggestAutoPosture implements F3: review-first for a store's first
// two weeks, then offer auto — once, never nagging.
func maybeSuggestAutoPosture(e *env) {
	if e.Config.Digest.Posture != "review-first" {
		return
	}
	created, _ := e.Store.GetMeta(store.MetaStoreCreated)
	if created == "" {
		return
	}
	t, err := time.Parse(time.RFC3339, created)
	if err != nil || time.Since(t) < 14*24*time.Hour {
		return
	}
	if nudged, _ := e.Store.GetMeta(store.MetaPostureNudged); nudged != "" {
		return
	}
	_ = e.Store.SetMeta(store.MetaPostureNudged, time.Now().UTC().Format(time.RFC3339))
	fmt.Printf("\nThis store is over two weeks old and digests still route through the\n" +
		"review inbox. If your approvals are mostly rubber stamps:\n" +
		"  amber config set digest.posture auto\n" +
		"(T3 untrusted-origin memories stay quarantined regardless.)\n")
}

func offOn(b bool) string {
	if b {
		return "local counters on (nothing is sent anywhere)"
	}
	return "off (zero telemetry)"
}
