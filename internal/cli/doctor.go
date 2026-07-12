package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/ghostlygawd/amber/internal/belief"
	"github.com/ghostlygawd/amber/internal/config"
	"github.com/ghostlygawd/amber/internal/embed"
	"github.com/ghostlygawd/amber/internal/store"
	"github.com/ghostlygawd/amber/internal/version"
)

func cmdDoctor() *cobra.Command {
	var (
		stale      bool
		reembed    bool
		fetchModel bool
	)
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Integrity, migration, and staleness report (never nags)",
		Long: `Checks database integrity, schema version, FTS consistency, embedding
model identity, and file permissions. --stale previews aging candidates.
--reembed migrates the store to the configured embedding model (mixed-
model stores are refused at search time until you do). --fetch-model
downloads the local embedding model.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if fetchModel {
				return doFetchModel()
			}
			e, err := openEnv()
			if err != nil {
				return err
			}
			defer e.Close()

			issues := 0
			report := func(ok bool, label, detail string) {
				mark := "ok"
				if !ok {
					mark = "!!"
					issues++
				}
				fmt.Printf("%-2s  %-24s %s\n", mark, label, detail)
			}

			// integrity
			var integrity string
			if err := e.Store.DB.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil {
				integrity = err.Error()
			}
			report(integrity == "ok", "sqlite integrity", integrity)

			// schema
			sv, _ := e.Store.GetMeta(store.MetaSchemaVersion)
			report(sv == fmt.Sprint(version.SchemaVersion), "schema version",
				fmt.Sprintf("store %s, binary %d", sv, version.SchemaVersion))

			// fts row parity
			var memN, ftsN int
			_ = e.Store.DB.QueryRow(`SELECT COUNT(*) FROM memories`).Scan(&memN)
			_ = e.Store.DB.QueryRow(`SELECT COUNT(*) FROM memories_fts`).Scan(&ftsN)
			report(memN == ftsN, "fts index", fmt.Sprintf("%d memories, %d indexed", memN, ftsN))

			// embedding identity
			storeModel, _ := e.Store.GetMeta(store.MetaEmbeddingModel)
			var missing int
			_ = e.Store.DB.QueryRow(`SELECT COUNT(*) FROM memories WHERE embedding IS NULL AND status IN ('active','aging')`).Scan(&missing)
			switch {
			case e.Embedder == nil:
				report(true, "embeddings", "BM25-only floor (no provider configured)")
			case storeModel == "":
				report(missing == memN || missing == 0, "embeddings", fmt.Sprintf("provider %s; %d rows missing vectors", e.Embedder.Name(), missing))
			case storeModel != e.Embedder.Name():
				report(false, "embeddings", fmt.Sprintf("store built with %s, config says %s — run `amber doctor --reembed`", storeModel, e.Embedder.Name()))
			default:
				report(missing == 0, "embeddings", fmt.Sprintf("%s; %d rows missing vectors", storeModel, missing))
			}

			// permissions
			fi, err := os.Stat(e.Store.Path)
			permOK := err == nil && fi.Mode().Perm()&0o077 == 0
			report(permOK, "file permissions", fmt.Sprintf("%s %v", e.Store.Path, fiMode(fi)))

			// orphans
			var orphans int
			_ = e.Store.DB.QueryRow(`SELECT COUNT(*) FROM memory_entities me WHERE NOT EXISTS (SELECT 1 FROM memories m WHERE m.id = me.memory_id)`).Scan(&orphans)
			report(orphans == 0, "orphaned links", fmt.Sprint(orphans))

			if stale {
				fmt.Println("\naging candidates (would demote on next consolidate; never deleted):")
				now := time.Now().UTC()
				ms, err := e.Store.List(store.ListFilter{Statuses: []string{store.StatusActive}})
				if err != nil {
					return err
				}
				n := 0
				for _, m := range ms {
					if belief.EffectiveConfidence(m, now) < belief.AgingThreshold {
						fmt.Println("  " + memLine(m))
						n++
					}
				}
				if n == 0 {
					fmt.Println("  none")
				}
			}

			if reembed {
				if e.Embedder == nil {
					return fmt.Errorf("no embedding provider configured; set embedding.provider first")
				}
				fmt.Printf("\nre-embedding with %s…\n", e.Embedder.Name())
				n, err := reembedAll(e)
				if err != nil {
					return err
				}
				fmt.Printf("re-embedded %d memories\n", n)
			}

			if issues > 0 {
				return fmt.Errorf("%d issue%s found", issues, plural(issues, "", "s"))
			}
			fmt.Println("\nno issues found")
			return nil
		},
	}
	c.Flags().BoolVar(&stale, "stale", false, "list memories below the aging confidence threshold")
	c.Flags().BoolVar(&reembed, "reembed", false, "re-embed the whole store with the configured model")
	c.Flags().BoolVar(&fetchModel, "fetch-model", false, "download the local embedding model and enable it")
	return c
}

func fiMode(fi os.FileInfo) any {
	if fi == nil {
		return "missing"
	}
	return fi.Mode().Perm()
}

func reembedAll(e *env) (int, error) {
	ms, err := e.Store.List(store.ListFilter{})
	if err != nil {
		return 0, err
	}
	n := 0
	batch := make([]*store.Memory, 0, 64)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		texts := make([]string, len(batch))
		for i, m := range batch {
			texts[i] = m.Content
		}
		vecs, err := e.Embedder.EmbedBatch(texts)
		if err != nil {
			return err
		}
		for i, m := range batch {
			if err := e.Store.SetEmbedding(m.ID, vecs[i]); err != nil {
				return err
			}
			n++
		}
		batch = batch[:0]
		return nil
	}
	for _, m := range ms {
		batch = append(batch, m)
		if len(batch) == 64 {
			if err := flush(); err != nil {
				return n, err
			}
		}
	}
	if err := flush(); err != nil {
		return n, err
	}
	if err := e.Store.SetMeta(store.MetaEmbeddingModel, e.Embedder.Name()); err != nil {
		return n, err
	}
	_ = e.Store.SetMeta(store.MetaEmbeddingDims, fmt.Sprint(e.Embedder.Dims()))
	_ = e.Store.AppendOp(store.OpReembed, "", map[string]any{"model": e.Embedder.Name(), "count": n})
	return n, nil
}

func doFetchModel() error {
	modelsDir, err := config.ModelsDir()
	if err != nil {
		return err
	}
	cwd, _ := os.Getwd()
	dir, _, err := config.ResolveStoreDir(config.Scope(flagScope), cwd)
	if err != nil {
		return err
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	model := cfg.Embedding.Model
	if model == "" {
		model = embed.DefaultLocalModel
	}
	if err := embed.FetchModel(modelsDir, model, cfg.Embedding.ModelURL, os.Stderr); err != nil {
		return err
	}
	m2v, err := embed.LoadModel2Vec(modelsDir, model)
	if err != nil {
		return err
	}
	cfg.Embedding.Provider = "local"
	cfg.Embedding.Model = model
	cfg.Embedding.Dims = m2v.Dims()
	if err := config.Save(dir, cfg); err != nil {
		return err
	}
	fmt.Printf("model %s ready (%d dims); embedding.provider=local\n", model, m2v.Dims())
	fmt.Println("run `amber doctor --reembed` if the store already has memories")
	return nil
}
