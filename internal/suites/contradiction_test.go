package suites

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ghostlygawd/amber/internal/store"
	"github.com/ghostlygawd/amber/internal/trust"
	"github.com/ghostlygawd/amber/internal/writer"
)

type contradictionSuite struct {
	Description string `json:"description"`
	Pairs       []struct {
		A        string   `json:"a"`
		B        string   `json:"b"`
		Type     string   `json:"type"`
		Entities []string `json:"entities"`
	} `json:"pairs"`
}

// TestContradictionSuite (§13 week-3 acceptance): 20 update pairs, ≥90%
// correct supersedence, 0 hard deletions.
func TestContradictionSuite(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "contradictions.json"))
	if err != nil {
		t.Fatal(err)
	}
	var suite contradictionSuite
	if err := json.Unmarshal(raw, &suite); err != nil {
		t.Fatal(err)
	}
	if len(suite.Pairs) < 20 {
		t.Fatalf("suite must have ≥20 pairs, has %d", len(suite.Pairs))
	}

	correct := 0
	for _, pair := range suite.Pairs {
		w := testWriter(t) // fresh store per pair: pairs must not interfere
		outA, err := w.Write(writer.Input{Content: pair.A, Type: pair.Type, Trust: trust.T0, Entities: pair.Entities})
		if err != nil {
			t.Fatalf("write A %q: %v", pair.A, err)
		}
		outB, err := w.Write(writer.Input{Content: pair.B, Type: pair.Type, Trust: trust.T0, Entities: pair.Entities})
		if err != nil {
			t.Fatalf("write B %q: %v", pair.B, err)
		}

		// Hard-deletion check: both rows must still exist regardless of verdict.
		var rows int
		if err := w.Store.DB.QueryRow(`SELECT COUNT(*) FROM memories`).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		wantRows := 2
		if outB.Action == "reconfirmed" {
			wantRows = 1
		}
		if rows != wantRows {
			t.Fatalf("HARD DELETION: %q/%q left %d rows, want %d", pair.A, pair.B, rows, wantRows)
		}

		oldA, err := w.Store.Get(outA.Memory.ID)
		if err != nil {
			t.Fatal(err)
		}
		if outB.Action == "superseded" && oldA.Status == store.StatusSuperseded && oldA.SupersededBy == outB.Memory.ID {
			correct++
		} else {
			t.Logf("MISS: %q → %q gave action=%s aStatus=%s", pair.A, pair.B, outB.Action, oldA.Status)
		}
	}

	score := float64(correct) / float64(len(suite.Pairs))
	t.Logf("contradiction suite: %d/%d correct supersedence (%.0f%%)", correct, len(suite.Pairs), score*100)
	if score < 0.9 {
		t.Fatalf("contradiction suite below gate: %.0f%% < 90%%", score*100)
	}
}
