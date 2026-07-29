package cli

import (
	"testing"

	"github.com/ghostlygawd/amber/internal/embed"
	"github.com/ghostlygawd/amber/internal/store"
	"github.com/ghostlygawd/amber/internal/trust"
)

func TestReembedAllUsesMigrationEmbedderAndExcludesNonSearchableRows(t *testing.T) {
	s, err := store.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	active := &store.Memory{Content: "active memory", Trust: trust.T0, Status: store.StatusActive}
	quarantined := &store.Memory{Content: "quarantined secret", Trust: trust.T3, Status: store.StatusQuarantined}
	for _, m := range []*store.Memory{active, quarantined} {
		if err := s.Insert(m, nil, nil); err != nil {
			t.Fatal(err)
		}
		if err := s.SetEmbedding(m.ID, []float32{9, 9}); err != nil {
			t.Fatal(err)
		}
	}

	e := &env{
		Store:             s,
		Embedder:          nil,
		MigrationEmbedder: embed.NewHash(8),
	}
	n, err := reembedAll(e)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("re-embedded %d memories, want 1", n)
	}

	gotActive, err := s.Get(active.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotActive.Embedding) != 8 {
		t.Fatalf("active vector has %d dimensions, want 8", len(gotActive.Embedding))
	}
	gotQuarantined, err := s.Get(quarantined.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotQuarantined.Embedding) != 0 {
		t.Fatalf("quarantined vector retained %d dimensions", len(gotQuarantined.Embedding))
	}
	model, err := s.GetMeta(store.MetaEmbeddingModel)
	if err != nil {
		t.Fatal(err)
	}
	if model != e.MigrationEmbedder.Name() {
		t.Fatalf("model=%q, want %q", model, e.MigrationEmbedder.Name())
	}
	ops, err := s.RecentOps(1, store.OpReembed)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 {
		t.Fatalf("got %d reembed operations, want 1", len(ops))
	}
}
