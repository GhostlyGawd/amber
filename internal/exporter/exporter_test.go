package exporter

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/ghostlygawd/amber/internal/store"
	"github.com/ghostlygawd/amber/internal/trust"
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
	res, err := ImportJSONL(&buf, dst, func(rec Record) error {
		m := &store.Memory{
			Content: rec.Content, Type: rec.Type, Importance: rec.Importance,
			Trust: trust.Tier(rec.Trust), Confidence: rec.Confidence, Status: rec.Status,
			CreatedAt: mustTime(rec.CreatedAt), UpdatedAt: mustTime(rec.UpdatedAt),
		}
		return dst.Insert(m, nil, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 3 || len(res.Errors) != 0 {
		t.Fatalf("import: %+v", res)
	}
	// Idempotent: re-import skips all by content hash.
	var buf2 bytes.Buffer
	_ = WriteJSONL(&buf2, ms)
	res2, err := ImportJSONL(&buf2, dst, func(rec Record) error { t.Fatal("must not insert"); return nil })
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
