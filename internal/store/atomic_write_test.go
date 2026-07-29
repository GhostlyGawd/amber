package store

import (
	"testing"

	"github.com/ghostlygawd/amber/internal/trust"
)

func TestAtomicWriteRollsBackPartialInsert(t *testing.T) {
	s, err := Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	_, err = s.AtomicWrite(AtomicWriteRequest{
		Memory: &Memory{Content: "rollback candidate", Trust: trust.T0, Confidence: 1},
		Decide: func([]*Memory) WriteDecision {
			return WriteDecision{Kind: WriteSupersedes}
		},
	})
	if err == nil {
		t.Fatal("expected invalid supersedence failure")
	}
	var memories, fts, ops int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM memories`).Scan(&memories); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM memories_fts`).Scan(&fts); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM ops WHERE op=?`, OpCreate).Scan(&ops); err != nil {
		t.Fatal(err)
	}
	if memories != 0 || fts != 0 || ops != 0 {
		t.Fatalf("partial write remained: memories=%d fts=%d create_ops=%d", memories, fts, ops)
	}
}

func TestReplaceEmbeddingsRollsBackModelAndVectors(t *testing.T) {
	s, err := Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	m := &Memory{
		Content: "existing vector", Trust: trust.T0, Confidence: 1,
		Status: StatusActive, Embedding: []float32{1, 2},
	}
	if err := s.Insert(m, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMeta(MetaEmbeddingModel, "old-model"); err != nil {
		t.Fatal(err)
	}

	err = s.ReplaceEmbeddings([]EmbeddingUpdate{
		{ID: m.ID, Vector: []float32{3, 4, 5}},
		{ID: "missing-memory", Vector: []float32{6, 7, 8}},
	}, "new-model", 3)
	if err == nil {
		t.Fatal("expected migration failure")
	}
	got, err := s.Get(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Embedding) != 2 || got.Embedding[0] != 1 || got.Embedding[1] != 2 {
		t.Fatalf("vector changed after rollback: %v", got.Embedding)
	}
	model, err := s.GetMeta(MetaEmbeddingModel)
	if err != nil {
		t.Fatal(err)
	}
	if model != "old-model" {
		t.Fatalf("model changed after rollback: %q", model)
	}
	ops, err := s.RecentOps(10, OpReembed)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 0 {
		t.Fatalf("migration journaled after rollback: %d operations", len(ops))
	}
}
