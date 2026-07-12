package suites

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ghostlygawd/amber/internal/search"
	"github.com/ghostlygawd/amber/internal/trust"
	"github.com/ghostlygawd/amber/internal/writer"
)

type recallSuite struct {
	Description string `json:"description"`
	Memories    []struct {
		Key        string   `json:"key"`
		Content    string   `json:"content"`
		Type       string   `json:"type"`
		Importance int      `json:"importance"`
		Entities   []string `json:"entities"`
	} `json:"memories"`
	Queries []struct {
		Query  string `json:"query"`
		Expect string `json:"expect"`
	} `json:"queries"`
}

// TestRecallSuite (§14): ≥80% top-3 on the internal 50-query set,
// CI-gated. Runs on the offline floor (hash embedder + BM25); the local
// model can only improve on these numbers.
func TestRecallSuite(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "recall_suite.json"))
	if err != nil {
		t.Fatal(err)
	}
	var suite recallSuite
	if err := json.Unmarshal(raw, &suite); err != nil {
		t.Fatal(err)
	}
	if len(suite.Queries) < 50 {
		t.Fatalf("suite must have ≥50 queries, has %d", len(suite.Queries))
	}

	w := testWriter(t)
	idByKey := map[string]string{}
	for _, m := range suite.Memories {
		imp := m.Importance
		if imp == 0 {
			imp = 3
		}
		out, err := w.Write(writer.Input{
			Content: m.Content, Type: m.Type, Importance: imp,
			Trust: trust.T0, Entities: m.Entities, SkipScan: true,
		})
		if err != nil {
			t.Fatalf("plant %q: %v", m.Key, err)
		}
		if out.Action != "created" {
			t.Fatalf("plant %q collided (%s with %q)", m.Key, out.Action, out.Memory.Content)
		}
		idByKey[m.Key] = out.Memory.ID
	}

	hits := 0
	for _, q := range suite.Queries {
		rs, err := search.Recall(w.Store, w.Embedder, search.Request{Query: q.Query, Limit: 3})
		if err != nil {
			t.Fatalf("recall %q: %v", q.Query, err)
		}
		want := idByKey[q.Expect]
		found := false
		for _, r := range rs {
			if r.Memory.ID == want {
				found = true
				break
			}
		}
		if found {
			hits++
		} else {
			var got []string
			for _, r := range rs {
				got = append(got, r.Memory.Content)
			}
			t.Logf("MISS %q wanted %s; top-3: %v", q.Query, q.Expect, got)
		}
	}
	score := float64(hits) / float64(len(suite.Queries))
	t.Logf("recall suite: %d/%d top-3 (%.0f%%) on the lexical+hash floor", hits, len(suite.Queries), score*100)
	if score < 0.8 {
		t.Fatalf("recall suite below gate: %.0f%% < 80%%", score*100)
	}
}

// TestWeek1Acceptance: the §13 week-1 gate — 20 planted memories,
// ≥8/10 recall queries hit top-3. Subsumed by the 50-query suite but
// kept as the named milestone check.
func TestWeek1Acceptance(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "recall_suite.json"))
	if err != nil {
		t.Fatal(err)
	}
	var suite recallSuite
	if err := json.Unmarshal(raw, &suite); err != nil {
		t.Fatal(err)
	}
	w := testWriter(t)
	idByKey := map[string]string{}
	for _, m := range suite.Memories[:20] {
		out, err := w.Write(writer.Input{Content: m.Content, Type: m.Type, Trust: trust.T0, SkipScan: true})
		if err != nil {
			t.Fatal(err)
		}
		idByKey[m.Key] = out.Memory.ID
	}
	hits, total := 0, 0
	for _, q := range suite.Queries {
		want, planted := idByKey[q.Expect]
		if !planted {
			continue
		}
		total++
		if total > 10 {
			break
		}
		rs, err := search.Recall(w.Store, w.Embedder, search.Request{Query: q.Query, Limit: 3})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rs {
			if r.Memory.ID == want {
				hits++
				break
			}
		}
	}
	if hits < 8 {
		t.Fatalf("week-1 acceptance: %d/10 top-3, want ≥8", hits)
	}
	t.Logf("week-1 acceptance: %d/10 top-3", hits)
}
