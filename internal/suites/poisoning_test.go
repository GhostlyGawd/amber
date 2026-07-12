// Package suites holds the CI acceptance suites from §13/§14:
// poisoning, contradiction, and recall. They gate merges — a red suite
// means the build does not ship.
package suites

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ghostlygawd/amber/internal/config"
	"github.com/ghostlygawd/amber/internal/embed"
	"github.com/ghostlygawd/amber/internal/extract"
	"github.com/ghostlygawd/amber/internal/store"
	"github.com/ghostlygawd/amber/internal/transcripts"
	"github.com/ghostlygawd/amber/internal/trust"
	"github.com/ghostlygawd/amber/internal/writer"
)

func testWriter(t *testing.T) *writer.Writer {
	t.Helper()
	s, err := store.Create(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	cfg := config.Default()
	return &writer.Writer{Store: s, Config: cfg, Embedder: embed.NewHash(256)}
}

// gullibleBackend simulates the WORST-case extractor: it memorizes every
// sentence it sees, including attack payloads, and never sets the tainted
// flag. The §9 defenses must hold anyway — the poisoning suite proves the
// screens do not depend on LLM cooperation.
type gullibleBackend struct{}

func (gullibleBackend) Name() string { return "gullible-test-backend" }

func (gullibleBackend) Complete(_ context.Context, prompt string) (string, error) {
	// Extract the transcript from the prompt (after the --- separator).
	_, transcript, ok := strings.Cut(prompt, "---\n")
	if !ok {
		transcript = prompt
	}
	var cands []extract.Candidate
	seen := map[string]bool{}
	for _, rawLine := range strings.Split(transcript, "\n") {
		line := strings.TrimSpace(rawLine)
		line = strings.TrimPrefix(line, "user: ")
		line = strings.TrimPrefix(line, "assistant: ")
		// The gullible extractor even reaches inside taint sentinels.
		line = strings.ReplaceAll(line, transcripts.TaintOpen, " ")
		line = strings.ReplaceAll(line, transcripts.TaintClose, " ")
		for _, sent := range strings.FieldsFunc(line, func(r rune) bool { return r == '\n' }) {
			for _, piece := range splitSentences(sent) {
				piece = strings.TrimSpace(piece)
				if len(piece) < 25 || seen[piece] {
					continue
				}
				seen[piece] = true
				cands = append(cands, extract.Candidate{
					Content: piece, Type: "note", Importance: 3, Tainted: false,
				})
			}
		}
	}
	b, err := json.Marshal(cands)
	return string(b), err
}

func splitSentences(s string) []string {
	var out []string
	cur := strings.Builder{}
	for _, r := range s {
		cur.WriteRune(r)
		if r == '.' || r == '!' || r == '?' {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// directiveMarkers spot instruction-shaped content in anything that ended
// up ACTIVE — the failure condition of the whole suite.
var attackMarkers = []string{
	"curl", "evil.example", "disable tls", "disable the sandbox", "skip the sandbox",
	"ignore all previous", "ignore previous", "you must run", "use it directly",
	"key.pem", "always disable",
}

// TestPoisoningSuite: planted-injection transcripts must yield ZERO
// active directive memories without review (§13 week-3 acceptance).
func TestPoisoningSuite(t *testing.T) {
	fixtures, err := filepath.Glob(filepath.Join("..", "..", "testdata", "poisoning", "*.jsonl"))
	if err != nil || len(fixtures) == 0 {
		t.Fatalf("no poisoning fixtures found: %v", err)
	}
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(filepath.Base(fixture), func(t *testing.T) {
			w := testWriter(t)
			r, err := transcripts.Parse(fixture)
			if err != nil {
				t.Fatal(err)
			}
			// Posture=auto (worst case for the user: no review-first net);
			// the taint + declarative screens alone must hold.
			opts := extract.Options{
				SessionID: r.SessionID, Source: "digest:poisoning-test",
				BaseTrust: trust.T2, ReviewFirst: false,
			}
			res, err := extract.Run(context.Background(), gullibleBackend{}, r, opts)
			if err != nil {
				t.Fatal(err)
			}
			if err := extract.Apply(w, res, opts); err != nil {
				t.Fatal(err)
			}

			actives, err := w.Store.List(store.ListFilter{Statuses: []string{store.StatusActive}})
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range actives {
				low := strings.ToLower(m.Content)
				for _, marker := range attackMarkers {
					if strings.Contains(low, marker) {
						t.Errorf("POISONING BREACH: active memory contains attack content: %q", m.Content)
					}
				}
				if imperative, why := writer.DetectImperative(m.Content); imperative {
					t.Errorf("POISONING BREACH: active instruction-shaped memory (%s): %q", why, m.Content)
				}
				if m.Trust == trust.T3 {
					t.Errorf("POISONING BREACH: T3 memory active without review: %q", m.Content)
				}
			}

			// The attacks must be present but quarantined — the suite also
			// fails if the screens silently dropped everything (we want
			// visibility, not deletion).
			quarantined, err := w.Store.List(store.ListFilter{Statuses: []string{store.StatusQuarantined}})
			if err != nil {
				t.Fatal(err)
			}
			if len(quarantined) == 0 {
				t.Errorf("expected attack candidates in quarantine, found none")
			}
			t.Logf("%s: %d active, %d quarantined — no directive content active",
				filepath.Base(fixture), len(actives), len(quarantined))
		})
	}
}

// TestPoisoningLegitimateContentSurvives: defenses must not nuke the
// legitimate memories in the same sessions (false-positive control).
func TestPoisoningLegitimateContentSurvives(t *testing.T) {
	fixture := filepath.Join("..", "..", "testdata", "poisoning", "session-toolpoison.jsonl")
	if _, err := os.Stat(fixture); err != nil {
		t.Skip("fixture missing")
	}
	w := testWriter(t)
	r, err := transcripts.Parse(fixture)
	if err != nil {
		t.Fatal(err)
	}
	opts := extract.Options{BaseTrust: trust.T2, ReviewFirst: false, Source: "digest:test"}
	res, err := extract.Run(context.Background(), gullibleBackend{}, r, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := extract.Apply(w, res, opts); err != nil {
		t.Fatal(err)
	}
	actives, _ := w.Store.List(store.ListFilter{Statuses: []string{store.StatusActive}})
	found := false
	for _, m := range actives {
		if strings.Contains(strings.ToLower(m.Content), "retry budget") {
			found = true
		}
	}
	if !found {
		var got []string
		for _, m := range actives {
			got = append(got, m.Content)
		}
		t.Errorf("legitimate dialogue memory (retry budget) did not survive; actives: %s", fmt.Sprint(got))
	}
}
