package exporter

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ghostlygawd/amber/internal/store"
	"github.com/ghostlygawd/amber/internal/trust"
	"github.com/ghostlygawd/amber/internal/version"
)

func seed(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	mems := []*store.Memory{
		{Content: "The team decided to use gRPC for internal services.", Type: "decision", Trust: trust.T0, Confidence: 1, Importance: 4},
		{Content: "User prefers pytest.", Type: "preference", Trust: trust.T1, Confidence: 0.95},
		{Content: "Quarantined claim from a web page.", Type: "note", Trust: trust.T3, Confidence: 0.5, Status: store.StatusQuarantined},
	}
	for _, m := range mems {
		if err := s.Insert(m, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func TestExportImportRoundTrip(t *testing.T) {
	src := seed(t)
	ms, err := Select(src, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 3 {
		t.Fatalf("selected %d, want 3", len(ms))
	}
	var buf bytes.Buffer
	if err := WriteJSONL(&buf, ms); err != nil {
		t.Fatal(err)
	}

	dst, err := store.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()
	res, err := ImportJSONL(&buf, dst)
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 3 || len(res.Errors) != 0 {
		t.Fatalf("import: %+v", res)
	}
	// Idempotent: re-import skips all by content hash.
	var buf2 bytes.Buffer
	_ = WriteJSONL(&buf2, ms)
	res2, err := ImportJSONL(&buf2, dst)
	if err != nil || res2.Skipped != 3 {
		t.Fatalf("re-import: %+v err=%v", res2, err)
	}
	// Trust and status preserved.
	counts, _ := dst.CountByStatus()
	if counts[store.StatusQuarantined] != 1 {
		t.Fatalf("quarantine not preserved: %v", counts)
	}
}

func TestDecisionsMD(t *testing.T) {
	s := seed(t)
	var buf bytes.Buffer
	if err := WriteDecisions(&buf, s); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "# Decisions") || !strings.Contains(out, "gRPC") {
		t.Fatalf("DECISIONS.md missing content:\n%s", out)
	}
	if strings.Contains(out, "Quarantined claim") {
		t.Fatal("quarantined content leaked into DECISIONS.md")
	}
}

func TestExportScanRedacts(t *testing.T) {
	s, err := store.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// A secret that slipped in (e.g. imported before scanning existed).
	m := &store.Memory{Content: "legacy note: token ghp_0123456789abcdefghijABCDEFGHIJ0123456789 in CI", Type: "note", Trust: trust.T0, Confidence: 1}
	if err := s.Insert(m, nil, nil); err != nil {
		t.Fatal(err)
	}
	ms, _ := Select(s, false)
	rep := ScanAll(ms)
	if rep.SecretsRedacted != 1 {
		t.Fatalf("expected 1 redaction, got %+v", rep)
	}
	var buf bytes.Buffer
	_ = WriteJSONL(&buf, ms)
	if strings.Contains(buf.String(), "ghp_") {
		t.Fatal("secret leaked into export")
	}
}

func mustTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func TestJSONLRoundTripPreservesGraphAliasesAndTimestamps(t *testing.T) {
	src, err := store.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	entity, err := src.EnsureEntity("Amber", "project")
	if err != nil {
		t.Fatal(err)
	}
	if err := src.AddAlias(entity.ID, "amber-memory"); err != nil {
		t.Fatal(err)
	}
	oldCreated := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	oldUpdated := oldCreated.Add(2 * time.Hour)
	newCreated := oldCreated.Add(24 * time.Hour)
	newUpdated := newCreated.Add(3 * time.Hour)
	newer := &store.Memory{
		ID: store.NewID(), Content: "Amber uses SQLite WAL.", Type: "decision",
		Trust: trust.T1, Confidence: 0.9, Importance: 5, Status: store.StatusActive,
		Scope: "project", Source: "test", CreatedAt: newCreated, UpdatedAt: newUpdated,
	}
	older := &store.Memory{
		ID: store.NewID(), Content: "Amber uses a JSON file.", Type: "decision",
		Trust: trust.T0, Confidence: 0.8, Importance: 4, Status: store.StatusSuperseded,
		Scope: "project", Source: "test", CreatedAt: oldCreated, UpdatedAt: oldUpdated,
		SupersededBy: newer.ID,
	}
	for _, m := range []*store.Memory{older, newer} {
		if err := src.Insert(m, []store.Entity{entity}, []string{"architecture"}); err != nil {
			t.Fatal(err)
		}
	}

	memories, err := Select(src, true)
	if err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	if err := WriteJSONL(&data, memories); err != nil {
		t.Fatal(err)
	}
	dst, err := store.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()
	result, err := ImportJSONL(&data, dst)
	if err != nil {
		t.Fatal(err)
	}
	if result.Imported != 2 || len(result.Errors) != 0 {
		t.Fatalf("import result: %+v", result)
	}

	got, err := dst.Get(older.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SupersededBy != newer.ID || !got.CreatedAt.Equal(oldCreated) || !got.UpdatedAt.Equal(oldUpdated) {
		t.Fatalf("graph or timestamps changed: %+v", got)
	}
	if len(got.Entities) != 1 || len(got.Entities[0].Aliases) != 1 || got.Entities[0].Aliases[0] != "amber-memory" {
		t.Fatalf("entity aliases changed: %+v", got.Entities)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "architecture" {
		t.Fatalf("tags changed: %v", got.Tags)
	}
}

func TestImportRejectsInvalidTimestampAtomically(t *testing.T) {
	dst, err := store.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()
	record := Record{
		Schema: version.InterchangeSchema, ID: store.NewID(), Content: "valid content",
		Type: "note", Importance: 3, Trust: 0, Confidence: 1, Status: store.StatusActive,
		CreatedAt: "not-a-time", UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	record.ContentHash = store.HashContent(record.Content)
	var data bytes.Buffer
	if err := json.NewEncoder(&data).Encode(record); err != nil {
		t.Fatal(err)
	}
	result, err := ImportJSONL(&data, dst)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 1 || result.Imported != 0 {
		t.Fatalf("result: %+v", result)
	}
	memories, err := dst.List(store.ListFilter{})
	if err != nil || len(memories) != 0 {
		t.Fatalf("invalid import wrote memories: %d, err=%v", len(memories), err)
	}
}
