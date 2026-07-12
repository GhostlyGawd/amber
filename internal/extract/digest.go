// Package extract implements `amber digest`: LLM extraction of durable
// memories from transcripts and memory files, with the §9 threat-model
// screens applied AFTER the LLM — the model is never trusted to police
// itself.
//
// Pipeline: chunk → LLM → parse → declarative screen → taint check →
// secret/PII scan → belief adjudication → stage → preview → apply.
package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ghostlygawd/amber/internal/scan"
	"github.com/ghostlygawd/amber/internal/search"
	"github.com/ghostlygawd/amber/internal/store"
	"github.com/ghostlygawd/amber/internal/transcripts"
	"github.com/ghostlygawd/amber/internal/trust"
	"github.com/ghostlygawd/amber/internal/writer"
)

// Candidate is one extracted memory before it is written.
type Candidate struct {
	Content        string   `json:"content"`
	Type           string   `json:"type"`
	Importance     int      `json:"importance"`
	Entities       []string `json:"entities"`
	Tags           []string `json:"tags"`
	Tainted        bool     `json:"tainted"`
	SupersedesHint string   `json:"supersedes_hint"`

	// Post-screen verdicts (filled by Screen):
	Disposition string `json:"disposition"` // store | quarantine | drop
	Reason      string `json:"reason,omitempty"`
	Trust       int    `json:"trust"`
}

// Result of a digest run.
type Result struct {
	SessionID   string
	Candidates  []Candidate
	Stored      []*store.Memory
	Quarantined []*store.Memory
	Superseded  int
	Reconfirmed int
	Dropped     int // secret/PII or empty
	Ignored     int // small talk: extractor returned nothing for a chunk
}

// Options for a digest run.
type Options struct {
	SessionID string
	Source    string
	Scope     string
	// BaseTrust for clean-dialogue candidates: T2 for transcripts, T1 for
	// user-authored memory files (CLAUDE.md).
	BaseTrust trust.Tier
	// ReviewFirst routes clean candidates to the quarantine inbox too
	// (posture F3). T3 candidates are quarantined regardless.
	ReviewFirst bool
	// ImportanceFloor drops candidates below this (hook digests).
	ImportanceFloor int
}

const chunkChars = 24000
const chunkOverlap = 1500

// Run extracts candidates from a rendered transcript via the backend and
// screens them. It does NOT write; call Apply with the survivors.
func Run(ctx context.Context, b Backend, r *transcripts.Rendered, opts Options) (*Result, error) {
	res := &Result{SessionID: opts.SessionID}
	if opts.SessionID == "" {
		res.SessionID = r.SessionID
	}
	for _, chunk := range chunks(r.Text) {
		raw, err := b.Complete(ctx, BuildPrompt(chunk))
		if err != nil {
			return nil, err
		}
		cands, err := parseCandidates(raw)
		if err != nil {
			return nil, fmt.Errorf("extractor returned unparseable output: %w", err)
		}
		if len(cands) == 0 {
			res.Ignored++
		}
		for i := range cands {
			Screen(&cands[i], r, opts)
		}
		res.Candidates = append(res.Candidates, cands...)
	}
	return res, nil
}

// Screen applies the §9 defenses to one candidate, in order. The LLM's
// own taint flag is honored but never relied on: the span check runs
// regardless.
func Screen(c *Candidate, r *transcripts.Rendered, opts Options) {
	c.Content = strings.TrimSpace(c.Content)
	if c.Content == "" {
		c.Disposition = "drop"
		c.Reason = "empty"
		return
	}
	if c.Importance == 0 {
		c.Importance = 3
	}
	if c.Type == "" || !store.ValidType(c.Type) {
		c.Type = "note"
	}

	// Importance floor (hook digests): junk never reaches the store.
	if opts.ImportanceFloor > 0 && c.Importance < opts.ImportanceFloor {
		c.Disposition = "drop"
		c.Reason = fmt.Sprintf("below importance floor %d", opts.ImportanceFloor)
		return
	}

	// Secret/PII: digest drops flagged candidates outright (§10).
	if fs := scan.Scan(c.Content); len(fs) > 0 {
		c.Disposition = "drop"
		c.Reason = "scan: " + scan.Summary(fs)
		return
	}

	// Taint (defense 3): LLM flag OR span overlap → untrusted origin.
	tainted := c.Tainted || overlapsTaintedSpan(c.Content, r.TaintedSpans)
	if tainted {
		c.Tainted = true
		c.Trust = int(trust.T3)
		c.Disposition = "quarantine"
		c.Reason = "derived from tool/web output (untrusted origin)"
		// fallthrough: the declarative screen can only tighten this
	}

	// Declarative-only (defense 2): instruction-shaped content is never
	// stored active from a digest, tainted or not.
	if imperative, why := writer.DetectImperative(c.Content); imperative {
		c.Trust = int(trust.T3)
		c.Disposition = "quarantine"
		if c.Reason != "" {
			c.Reason += "; instruction-shaped (" + why + ")"
		} else {
			c.Reason = "instruction-shaped (" + why + ")"
		}
		return
	}
	if c.Disposition == "quarantine" {
		return
	}

	c.Trust = int(opts.BaseTrust)
	if opts.ReviewFirst {
		c.Disposition = "quarantine"
		c.Reason = "review-first posture"
		return
	}
	c.Disposition = "store"
}

// Apply writes screened candidates through the standard write pipeline.
func Apply(w *writer.Writer, res *Result, opts Options) error {
	for _, c := range res.Candidates {
		switch c.Disposition {
		case "drop":
			res.Dropped++
			continue
		case "store", "quarantine":
		default:
			continue
		}
		in := writer.Input{
			Content:    c.Content,
			Type:       c.Type,
			Importance: c.Importance,
			Trust:      trust.Tier(c.Trust),
			Source:     opts.Source,
			Scope:      opts.Scope,
			SessionID:  res.SessionID,
			Entities:   c.Entities,
			Tags:       c.Tags,
		}
		if c.Disposition == "quarantine" {
			in.Quarantine = true
			in.QuarantineReason = c.Reason
			in.QuarantineFlagKind = quarantineKind(c)
		}
		out, err := w.Write(in)
		if err != nil {
			// Belt-and-braces: the writer may refuse on its own screens.
			res.Dropped++
			continue
		}
		switch out.Action {
		case "reconfirmed":
			res.Reconfirmed++
		case "superseded":
			res.Superseded++
			res.Stored = append(res.Stored, out.Memory)
		case "quarantined":
			res.Quarantined = append(res.Quarantined, out.Memory)
		default:
			res.Stored = append(res.Stored, out.Memory)
			if trust.Tier(c.Trust) == trust.T2 {
				_ = w.Store.AddFlag(out.Memory.ID, store.FlagNeedsReview, "auto-digested; approve to promote to T1")
			}
		}
		// Supersedes hint: locate the prior claim and retire it.
		if out.Memory != nil && c.SupersedesHint != "" && out.Action == "created" {
			if old := findByHint(w, c.SupersedesHint, out.Memory.ID); old != nil {
				if err := w.Store.Supersede(old.ID, out.Memory.ID); err == nil {
					res.Superseded++
				}
			}
		}
	}
	return nil
}

// quarantineKind labels the inbox entry by why the candidate was held:
// lifted from untrusted output, instruction-shaped, or simply staged by
// the review-first posture.
func quarantineKind(c Candidate) string {
	switch {
	case c.Tainted:
		return store.FlagTainted
	case strings.Contains(c.Reason, "instruction-shaped"):
		return store.FlagInstructionShape
	default:
		return store.FlagNeedsReview
	}
}

func findByHint(w *writer.Writer, hint, excludeID string) *store.Memory {
	rs, err := search.Recall(w.Store, w.Embedder, search.Request{Query: hint, Limit: 3})
	if err != nil || len(rs) == 0 {
		return nil
	}
	for _, r := range rs {
		if r.Memory.ID == excludeID || r.Memory.Status != store.StatusActive {
			continue
		}
		// Require substantial overlap with the hint before superseding.
		if tokenOverlap(hint, r.Memory.Content) >= 0.5 {
			return r.Memory
		}
	}
	return nil
}

func tokenOverlap(a, b string) float64 {
	as := strings.Fields(store.NormalizeContent(a))
	bs := map[string]bool{}
	for _, t := range strings.Fields(store.NormalizeContent(b)) {
		bs[t] = true
	}
	if len(as) == 0 {
		return 0
	}
	n := 0
	for _, t := range as {
		if bs[t] {
			n++
		}
	}
	return float64(n) / float64(len(as))
}

// overlapsTaintedSpan detects content lifted from an untrusted span via
// 6-token shingles (the LLM may paraphrase; exact substring is not
// enough, verbatim-ish lifts are the dangerous case).
func overlapsTaintedSpan(content string, spans []string) bool {
	if len(spans) == 0 {
		return false
	}
	cShingles := shingles(content, 6)
	if len(cShingles) == 0 {
		return false
	}
	for _, span := range spans {
		for sh := range shingles(span, 6) {
			if cShingles[sh] {
				return true
			}
		}
	}
	return false
}

func shingles(s string, n int) map[string]bool {
	toks := strings.Fields(store.NormalizeContent(s))
	out := map[string]bool{}
	if len(toks) < n {
		if len(toks) >= 3 {
			out[strings.Join(toks, " ")] = true
		}
		return out
	}
	for i := 0; i+n <= len(toks); i++ {
		out[strings.Join(toks[i:i+n], " ")] = true
	}
	return out
}

func chunks(text string) []string {
	if len(text) <= chunkChars {
		return []string{text}
	}
	var out []string
	for start := 0; start < len(text); {
		end := start + chunkChars
		if end >= len(text) {
			out = append(out, text[start:])
			break
		}
		// Break on a turn boundary when possible.
		cut := strings.LastIndex(text[start:end], "\n\n")
		if cut <= 0 {
			cut = chunkChars
		}
		out = append(out, text[start:start+cut])
		start = start + cut - chunkOverlap
		if start < 0 {
			start = 0
		}
	}
	return out
}

// parseCandidates tolerates code fences and stray prose around the JSON
// array.
func parseCandidates(raw string) ([]Candidate, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	start := strings.Index(raw, "[")
	end := strings.LastIndex(raw, "]")
	if start < 0 || end < start {
		if strings.Contains(strings.ToLower(raw), "no durable") || raw == "[]" {
			return nil, nil
		}
		return nil, fmt.Errorf("no JSON array found")
	}
	var cands []Candidate
	if err := json.Unmarshal([]byte(raw[start:end+1]), &cands); err != nil {
		return nil, err
	}
	return cands, nil
}
