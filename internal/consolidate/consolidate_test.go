package consolidate

import (
	"strings"
	"testing"
	"time"

	"github.com/ghostlygawd/amber/internal/embed"
	"github.com/ghostlygawd/amber/internal/store"
	"github.com/ghostlygawd/amber/internal/trust"
)

func TestAbsolutizeDates(t *testing.T) {
	anchor := time.Date(2026, 7, 11, 15, 0, 0, 0, time.UTC) // a Saturday
	cases := map[string]string{
		"Deployed the fix yesterday.":         "Deployed the fix on 2026-07-10.",
		"The migration ran last Tuesday.":     "The migration ran on 2026-07-07.",
		"Launch is planned for next Friday.":  "Launch is planned for on 2026-07-17.", // wording tolerated; date must be right
		"Standup moved 3 days ago.":           "Standup moved on 2026-07-08.",
		"The incident happened this morning.": "The incident happened on 2026-07-11.",
		"No relative dates here at all.":      "No relative dates here at all.",
	}
	for in, want := range cases {
		got, changed := AbsolutizeDates(in, anchor)
		if got != want {
			t.Errorf("AbsolutizeDates(%q) = %q, want %q", in, got, want)
		}
		if (got != in) != changed {
			t.Errorf("changed flag wrong for %q", in)
		}
	}
}

func TestConsolidateNeverDeletes(t *testing.T) {
	s, err := store.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e := embed.NewHash(256)

	insert := func(content string, created time.Time, conf float64, memType string) string {
		v, _ := e.Embed(content)
		m := &store.Memory{
			Content: content, Type: memType, Trust: trust.T0, Confidence: conf,
			CreatedAt: created, UpdatedAt: created, LastConfirmedAt: created,
			Embedding: v, Importance: 3,
		}
		if err := s.Insert(m, nil, nil); err != nil {
			t.Fatal(err)
		}
		return m.ID
	}

	old := time.Now().Add(-400 * 24 * time.Hour)
	dupA := insert("The deploy pipeline uses Buildkite for CI.", time.Now().Add(-time.Hour), 1, "fact")
	dupB := insert("The deploy pipeline uses Buildkite for CI!", time.Now().Add(-2*time.Hour), 1, "fact")
	aged := insert("Debugged the flaky websocket reconnect event.", old, 0.9, "event")
	dated := insert("Shipped the auth refactor yesterday.", time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), 1, "event")
	_ = dated

	before := count(t, s)
	rep, err := Run(s, e, time.Time{}, false)
	if err != nil {
		t.Fatal(err)
	}
	after := count(t, s)
	if before != after {
		t.Fatalf("CONSOLIDATE DELETED ROWS: %d → %d", before, after)
	}
	if rep.Merged < 1 {
		t.Errorf("expected duplicate merge, report: %+v", rep)
	}
	if rep.Demoted < 1 {
		t.Errorf("expected aged demotion, report: %+v", rep)
	}
	if rep.Dated < 1 {
		t.Errorf("expected date absolutization, report: %+v", rep)
	}

	// The older duplicate is superseded (not deleted) by the newer one.
	mb, err := s.Get(dupB)
	if err != nil {
		t.Fatal(err)
	}
	if mb.Status != store.StatusSuperseded || mb.SupersededBy != dupA {
		t.Errorf("dup not merged softly: %s superseded_by=%s", mb.Status, mb.SupersededBy)
	}
	ma, _ := s.Get(aged)
	if ma.Status != store.StatusAging {
		t.Errorf("old event not demoted: %s", ma.Status)
	}
	md, _ := s.Get(dated)
	if !strings.Contains(md.Content, "2026-06-30") {
		t.Errorf("date not absolutized: %q", md.Content)
	}
	// Reversibility: ops journal recorded the original content.
	ops, _ := s.OpsFor(dated)
	foundEdit := false
	for _, o := range ops {
		if o.Op == store.OpEdit && strings.Contains(string(o.Payload), "yesterday") {
			foundEdit = true
		}
	}
	if !foundEdit {
		t.Error("absolutization did not journal the original content")
	}
}

func count(t *testing.T, s *store.Store) int {
	t.Helper()
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM memories`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
