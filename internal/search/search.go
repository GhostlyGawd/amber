// Package search implements hybrid retrieval (§7): FTS5/BM25 lexical and
// brute-force cosine semantic candidates fused with reciprocal rank
// fusion, then modulated by importance × trust × per-type recency decay.
// Every recall can explain itself (--why): which memories, which scores,
// why included (decision D18).
package search

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ghostlygawd/amber/internal/belief"
	"github.com/ghostlygawd/amber/internal/embed"
	"github.com/ghostlygawd/amber/internal/store"
)

// Request describes one recall.
type Request struct {
	Query   string
	Limit   int
	Entity  string
	Types   []string
	Since   time.Time
	History bool // include superseded/tombstoned/quarantined
	Now     time.Time
}

// Result is one scored hit with full attribution.
type Result struct {
	Memory *store.Memory `json:"memory"`
	Score  float64       `json:"score"`
	Why    Attribution   `json:"why"`
}

// Attribution explains a hit (D18).
type Attribution struct {
	LexicalRank    int     `json:"lexical_rank,omitempty"`  // 1-based; 0 = not a lexical candidate
	SemanticRank   int     `json:"semantic_rank,omitempty"` // 1-based; 0 = not a semantic candidate
	BM25           float64 `json:"bm25,omitempty"`
	Cosine         float64 `json:"cosine,omitempty"`
	RRF            float64 `json:"rrf"`
	ImportanceMult float64 `json:"importance_mult"`
	TrustMult      float64 `json:"trust_mult"`
	RecencyMult    float64 `json:"recency_mult"`
	ConfidenceMult float64 `json:"confidence_mult"`
	Note           string  `json:"note,omitempty"`
}

const rrfK = 60.0

// Recall runs hybrid retrieval on one store. embedder may be nil
// (BM25-only floor).
func Recall(s *store.Store, e embed.Embedder, req Request) ([]Result, error) {
	if req.Now.IsZero() {
		req.Now = time.Now().UTC()
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 8
	}
	// Filters applied pre-fusion (§7): status set, type, entity, since.
	statuses := []string{store.StatusActive, store.StatusAging}
	if req.History {
		statuses = append(statuses, store.StatusSuperseded, store.StatusTombstoned, store.StatusQuarantined)
	}
	pool, err := s.List(store.ListFilter{Statuses: statuses, Types: req.Types, Entity: req.Entity, Since: req.Since})
	if err != nil {
		return nil, err
	}
	if len(pool) == 0 {
		return nil, nil
	}
	byID := make(map[string]*store.Memory, len(pool))
	for _, m := range pool {
		byID[m.ID] = m
	}

	candN := limit * 6
	if candN < 50 {
		candN = 50
	}

	lexRank, bm25 := lexicalRanks(s, req.Query, byID, candN)
	semRank, cosSim := semanticRanks(e, req.Query, pool, candN)

	// Fusion: reciprocal rank fusion over both lists.
	type fused struct {
		m *store.Memory
		a Attribution
	}
	fusedMap := map[string]*fused{}
	touch := func(id string) *fused {
		f, ok := fusedMap[id]
		if !ok {
			f = &fused{m: byID[id]}
			fusedMap[id] = f
		}
		return f
	}
	for id, r := range lexRank {
		f := touch(id)
		f.a.LexicalRank = r
		f.a.BM25 = bm25[id]
		f.a.RRF += 1.0 / (rrfK + float64(r))
	}
	for id, r := range semRank {
		f := touch(id)
		f.a.SemanticRank = r
		f.a.Cosine = cosSim[id]
		f.a.RRF += 1.0 / (rrfK + float64(r))
	}

	var results []Result
	for _, f := range fusedMap {
		if f.m == nil {
			continue
		}
		score, attr := modulate(f.m, f.a, req.Now)
		results = append(results, Result{Memory: f.m, Score: score, Why: attr})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Memory.UpdatedAt.After(results[j].Memory.UpdatedAt)
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// modulate applies §7 step 3: importance × trust × per-type recency decay,
// plus aging/confidence handling. Events decay (~90-day half-life);
// preferences/decisions don't decay — they supersede.
func modulate(m *store.Memory, a Attribution, now time.Time) (float64, Attribution) {
	a.ImportanceMult = 0.6 + 0.1*float64(m.Importance)
	a.TrustMult = m.Trust.RankFactor()
	a.RecencyMult = recencyFactor(m, now)
	eff := belief.EffectiveConfidence(m, now)
	a.ConfidenceMult = 0.5 + 0.5*eff
	if m.Status == store.StatusAging {
		a.ConfidenceMult *= 0.6
		a.Note = "aging: excluded from auto-injection, still recallable"
	}
	switch m.Status {
	case store.StatusSuperseded:
		a.ConfidenceMult *= 0.4
		a.Note = "superseded" + supersededBy(m)
	case store.StatusTombstoned:
		a.ConfidenceMult *= 0.25
		a.Note = "tombstoned (forgotten)"
	case store.StatusQuarantined:
		a.ConfidenceMult *= 0.4
		a.Note = "quarantined: untrusted origin, pending review"
	}
	return a.RRF * a.ImportanceMult * a.TrustMult * a.RecencyMult * a.ConfidenceMult, a
}

func supersededBy(m *store.Memory) string {
	if m.SupersededBy != "" {
		return " by " + m.SupersededBy
	}
	return ""
}

func recencyFactor(m *store.Memory, now time.Time) float64 {
	hl := belief.HalfLife(m.Type)
	if hl == 0 {
		return 1.0 // preferences/decisions: no decay
	}
	anchor := m.LastConfirmedAt
	if anchor.IsZero() {
		anchor = m.UpdatedAt
	}
	age := now.Sub(anchor)
	if age <= 0 {
		return 1.0
	}
	// Floor at 0.3: old events rank lower but stay findable.
	f := 0.3 + 0.7*halfDecay(age, hl)
	return f
}

func halfDecay(age, hl time.Duration) float64 {
	x := float64(age) / float64(hl)
	// 2^-x without math import churn
	v := 1.0
	for x >= 1 {
		v /= 2
		x--
	}
	// linear interpolation of the fractional half-life — close enough for
	// a ranking multiplier
	return v * (1 - 0.5*x)
}

// lexicalRanks runs FTS5 BM25 over the query, restricted to the filtered
// pool. Returns id→rank (1-based) and id→bm25 (negative = better in
// SQLite; we negate so higher = better).
func lexicalRanks(s *store.Store, query string, pool map[string]*store.Memory, n int) (map[string]int, map[string]float64) {
	ranks := map[string]int{}
	scores := map[string]float64{}
	match := ftsQuery(query)
	if match == "" {
		return ranks, scores
	}
	rows, err := s.DB.Query(`
		SELECT m.id, bm25(memories_fts) AS score
		FROM memories_fts JOIN memories m ON m.rowid = memories_fts.rowid
		WHERE memories_fts MATCH ?
		ORDER BY score LIMIT ?`, match, n)
	if err != nil {
		return ranks, scores // e.g. all-stopword query; fall back to semantic only
	}
	defer rows.Close()
	r := 0
	for rows.Next() {
		var id string
		var sc float64
		if err := rows.Scan(&id, &sc); err != nil {
			break
		}
		if _, ok := pool[id]; !ok {
			continue // filtered out (status/type/entity/since)
		}
		r++
		ranks[id] = r
		scores[id] = -sc
	}
	return ranks, scores
}

// ftsQuery sanitizes free text into an FTS5 OR-query of quoted terms, so
// user punctuation can't break MATCH syntax.
func ftsQuery(q string) string {
	fields := strings.Fields(store.NormalizeContent(q))
	if len(fields) == 0 {
		return ""
	}
	if len(fields) > 12 {
		fields = fields[:12]
	}
	quoted := make([]string, len(fields))
	for i, f := range fields {
		quoted[i] = `"` + strings.ReplaceAll(f, `"`, ``) + `"`
	}
	return strings.Join(quoted, " OR ")
}

// semanticRanks embeds the query and brute-force scans pool embeddings.
func semanticRanks(e embed.Embedder, query string, pool []*store.Memory, n int) (map[string]int, map[string]float64) {
	ranks := map[string]int{}
	sims := map[string]float64{}
	if e == nil || query == "" {
		return ranks, sims
	}
	qv, err := e.Embed(query)
	if err != nil {
		return ranks, sims
	}
	type hit struct {
		id  string
		sim float64
	}
	hits := make([]hit, 0, len(pool))
	for _, m := range pool {
		if len(m.Embedding) == 0 {
			continue
		}
		sim := embed.Cosine(qv, m.Embedding)
		if sim <= 0 {
			continue
		}
		hits = append(hits, hit{m.ID, sim})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].sim > hits[j].sim })
	if len(hits) > n {
		hits = hits[:n]
	}
	for i, h := range hits {
		ranks[h.id] = i + 1
		sims[h.id] = h.sim
	}
	return ranks, sims
}

// Briefing selects the session-start injection set with no query: active,
// injectable-trust memories ranked by importance × trust × recency ×
// confidence. Aging and quarantined are excluded by construction.
func Briefing(s *store.Store, now time.Time, maxItems int) ([]Result, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	pool, err := s.List(store.ListFilter{Statuses: []string{store.StatusActive}, Trusts: []int{0, 1, 2}})
	if err != nil {
		return nil, err
	}
	var results []Result
	for _, m := range pool {
		if !m.Trust.Injectable() {
			continue
		}
		a := Attribution{RRF: 1, Note: "session-start briefing (no query)"}
		score, attr := modulate(m, a, now)
		// Prefer preferences and decisions in a no-query briefing: they are
		// standing state, while events/notes are episodic.
		switch m.Type {
		case "preference", "decision":
			score *= 1.3
		case "fact":
			score *= 1.15
		}
		results = append(results, Result{Memory: m, Score: score, Why: attr})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if maxItems > 0 && len(results) > maxItems {
		results = results[:maxItems]
	}
	return results, nil
}

// MergeScopes fuses results from multiple stores (global + project),
// deduping identical content (project wins).
func MergeScopes(rs ...[]Result) []Result {
	seen := map[string]bool{}
	var out []Result
	for _, list := range rs {
		for _, r := range list {
			key := r.Memory.ContentHash
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

// FormatWhy renders an attribution as one man-page-voice line.
func FormatWhy(r Result) string {
	var parts []string
	if r.Why.LexicalRank > 0 {
		parts = append(parts, fmt.Sprintf("lexical #%d (bm25 %.2f)", r.Why.LexicalRank, r.Why.BM25))
	}
	if r.Why.SemanticRank > 0 {
		parts = append(parts, fmt.Sprintf("semantic #%d (cosine %.2f)", r.Why.SemanticRank, r.Why.Cosine))
	}
	if len(parts) == 0 {
		parts = append(parts, "briefing")
	}
	parts = append(parts, fmt.Sprintf("rrf %.4f", r.Why.RRF))
	parts = append(parts, fmt.Sprintf("×imp %.2f ×trust %.2f ×recency %.2f ×conf %.2f", r.Why.ImportanceMult, r.Why.TrustMult, r.Why.RecencyMult, r.Why.ConfidenceMult))
	line := strings.Join(parts, ", ")
	if r.Why.Note != "" {
		line += " — " + r.Why.Note
	}
	return line
}
