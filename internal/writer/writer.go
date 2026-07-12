// Package writer is the single write path for new memories. Every surface
// (CLI remember, MCP, digest apply, import) goes through Write so the
// same defenses apply everywhere: secret/PII scanning, declarative
// normalization, trust-tier quarantine, dedupe/supersedence adjudication.
package writer

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ghostlygawd/amber/internal/belief"
	"github.com/ghostlygawd/amber/internal/config"
	"github.com/ghostlygawd/amber/internal/embed"
	"github.com/ghostlygawd/amber/internal/entityx"
	"github.com/ghostlygawd/amber/internal/scan"
	"github.com/ghostlygawd/amber/internal/store"
	"github.com/ghostlygawd/amber/internal/trust"
)

// Input is a candidate memory.
type Input struct {
	Content    string
	Type       string
	Importance int
	Trust      trust.Tier
	Scope      string
	Source     string
	SessionID  string
	Entities   []string // explicit entity names (added to extracted ones)
	Tags       []string
	// Quarantine forces status=quarantined regardless of tier (digest
	// posture, instruction-shape screen).
	Quarantine bool
	// QuarantineReason recorded as a flag when Quarantine is set.
	QuarantineReason string
	// QuarantineFlagKind selects the review-inbox flag kind
	// (default store.FlagTainted).
	QuarantineFlagKind string
	// Force bypasses a scan refusal in warn mode (secrets still redacted).
	Force bool
	// SkipScan is for trusted internal rewrites (consolidation date fixes).
	SkipScan bool
	// Timestamps for imports; zero = now.
	CreatedAt       time.Time
	LastConfirmedAt time.Time
	Confidence      float64 // 0 = tier default
	Status          string  // import only; "" = derived
}

// Outcome reports what the write did.
type Outcome struct {
	Action     string        // created | reconfirmed | superseded | quarantined | refused
	Memory     *store.Memory // the resulting record (nil when refused)
	Superseded *store.Memory // set when Action == superseded
	Ambiguous  *store.Memory // set when kept-both was flagged
	Findings   []scan.Finding
	Normalized bool // imperative input rewritten as declarative preference
	Refusal    string
}

// ErrScanRefused is returned when the scanner refuses a write.
var ErrScanRefused = errors.New("write refused by secret/PII scan")

// Writer binds a store, its config, and an optional embedder.
type Writer struct {
	Store    *store.Store
	Config   *config.Config
	Embedder embed.Embedder // nil = BM25-only floor
}

// Write runs the full §8/§9 write pipeline.
func (w *Writer) Write(in Input) (*Outcome, error) {
	out := &Outcome{}
	content := strings.TrimSpace(in.Content)
	if content == "" {
		return nil, errors.New("empty content")
	}
	if len(content) > 4000 {
		return nil, fmt.Errorf("content too long (%d chars, max 4000) — memories are claims, not documents", len(content))
	}
	if in.Type != "" && !store.ValidType(in.Type) {
		return nil, fmt.Errorf("unknown type %q (want fact|preference|decision|event|note)", in.Type)
	}
	if in.Importance < 0 || in.Importance > 5 {
		return nil, fmt.Errorf("importance must be 1..5")
	}
	if !in.Trust.Valid() {
		return nil, fmt.Errorf("invalid trust tier %d", int(in.Trust))
	}

	// 1. Declarative-only constraint (§9 defense 2). A user instructing
	// themselves is legitimate: T0 imperatives are normalized into
	// declarative preferences. Auto/untrusted imperatives are quarantined.
	memType := in.Type
	if imperative, _ := DetectImperative(content); imperative {
		if in.Trust == trust.T0 {
			content, memType = NormalizeImperative(content, memType)
			out.Normalized = true
		} else {
			in.Quarantine = true
			if in.QuarantineReason == "" {
				in.QuarantineReason = "instruction-shaped content from non-user source"
			}
			if in.Trust != trust.T3 {
				in.Trust = trust.T3
			}
		}
	}

	// 2. Secret/PII scan on write (§10).
	if !in.SkipScan {
		findings := scan.Scan(content)
		out.Findings = findings
		if len(findings) > 0 {
			if w.Config.Scan.Mode == "block" {
				out.Action = "refused"
				out.Refusal = "scan.mode=block: " + scan.Summary(findings)
				return out, ErrScanRefused
			}
			if !in.Force {
				out.Action = "refused"
				out.Refusal = "detected " + scan.Summary(findings) + " (re-run with --force to store; secrets will be redacted)"
				return out, ErrScanRefused
			}
			if scan.HasSecrets(findings) {
				content = scan.Redact(content, findings)
			}
		}
	}

	// 3. Entities: explicit + heuristic extraction.
	known, _ := w.Store.ListEntities("")
	knownNames := make([]string, 0, len(known))
	for _, e := range known {
		knownNames = append(knownNames, e.Name)
	}
	mentions := entityx.Extract(content, knownNames)
	entNames := map[string]string{} // name -> type
	for _, m := range mentions {
		entNames[m.Name] = m.Type
	}
	for _, n := range in.Entities {
		if strings.TrimSpace(n) != "" {
			if _, ok := entNames[n]; !ok {
				entNames[n] = "other"
			}
		}
	}
	var ents []store.Entity
	for n, t := range entNames {
		e, err := w.Store.EnsureEntity(n, t)
		if err != nil {
			return nil, err
		}
		ents = append(ents, e)
	}
	if name, email := entityx.EmailAlias(content); name != "" {
		if e, err := w.Store.EnsureEntity(name, "person"); err == nil {
			_ = w.Store.AddAlias(e.ID, email)
		}
	}
	entNameList := make([]string, 0, len(ents))
	for _, e := range ents {
		entNameList = append(entNameList, e.Name)
	}

	// 4. Embed (nil embedder = lexical floor; belief falls back to token
	// overlap). The first embedded write pins the model identity in meta;
	// mixed-model stores are refused at open (§6).
	var vec []float32
	if w.Embedder != nil {
		v, err := w.Embedder.Embed(content)
		if err == nil {
			vec = v
			if pinned, _ := w.Store.GetMeta(store.MetaEmbeddingModel); pinned == "" {
				_ = w.Store.SetMeta(store.MetaEmbeddingModel, w.Embedder.Name())
				_ = w.Store.SetMeta(store.MetaEmbeddingDims, fmt.Sprint(w.Embedder.Dims()))
			}
		}
	}

	// 5. Belief adjudication against similar actives (skip for quarantined
	// writes: they are not beliefs yet).
	if !in.Quarantine && in.Status == "" {
		candidates := w.adjudicationPool()
		dec := belief.Adjudicate(content, vec, entNameList, candidates)
		switch dec.Verdict {
		case belief.VerdictDuplicate:
			if err := w.Store.Reconfirm(dec.Existing.ID, in.Importance); err != nil {
				return nil, err
			}
			m, err := w.Store.Get(dec.Existing.ID)
			if err != nil {
				return nil, err
			}
			out.Action = "reconfirmed"
			out.Memory = m
			return out, nil
		case belief.VerdictSupersedes:
			m, err := w.insert(in, content, memType, ents, vec)
			if err != nil {
				return nil, err
			}
			if err := w.Store.Supersede(dec.Existing.ID, m.ID); err != nil {
				return nil, err
			}
			out.Action = "superseded"
			out.Memory = m
			out.Superseded = dec.Existing
			return out, nil
		case belief.VerdictAmbiguous:
			m, err := w.insert(in, content, memType, ents, vec)
			if err != nil {
				return nil, err
			}
			_ = w.Store.AddFlag(m.ID, store.FlagAmbiguity,
				fmt.Sprintf("possibly contradicts %s: %q — %s", dec.Existing.ID, truncate(dec.Existing.Content, 120), dec.Reason))
			out.Action = "created"
			out.Memory = m
			out.Ambiguous = dec.Existing
			return out, nil
		}
	}

	m, err := w.insert(in, content, memType, ents, vec)
	if err != nil {
		return nil, err
	}
	if in.Quarantine {
		out.Action = "quarantined"
	} else {
		out.Action = "created"
	}
	out.Memory = m
	return out, nil
}

func (w *Writer) insert(in Input, content, memType string, ents []store.Entity, vec []float32) (*store.Memory, error) {
	status := in.Status
	if status == "" {
		status = store.StatusActive
		if in.Quarantine || in.Trust == trust.T3 {
			status = store.StatusQuarantined
		}
	}
	conf := in.Confidence
	if conf == 0 {
		conf = in.Trust.InitialConfidence()
	}
	m := &store.Memory{
		Content:         content,
		Type:            memType,
		Importance:      in.Importance,
		Trust:           in.Trust,
		Confidence:      conf,
		Scope:           in.Scope,
		Source:          in.Source,
		SessionID:       in.SessionID,
		Status:          status,
		Embedding:       vec,
		CreatedAt:       in.CreatedAt,
		LastConfirmedAt: in.LastConfirmedAt,
	}
	if m.Importance == 0 {
		m.Importance = 3
	}
	if err := w.Store.Insert(m, ents, in.Tags); err != nil {
		return nil, err
	}
	if status == store.StatusQuarantined {
		reason := in.QuarantineReason
		if reason == "" {
			reason = "untrusted origin (trust tier T3)"
		}
		kind := in.QuarantineFlagKind
		if kind == "" {
			kind = store.FlagTainted
		}
		_ = w.Store.AddFlag(m.ID, kind, reason)
		_ = w.Store.AppendOp(store.OpQuarantine, m.ID, map[string]any{"reason": reason})
	}
	return m, nil
}

// adjudicationPool returns the actives the belief engine compares a new
// claim against. Adjudicate ranks by similarity itself; the pool is capped
// to bound write-time cost on very large stores.
func (w *Writer) adjudicationPool() []*store.Memory {
	pool, err := w.Store.List(store.ListFilter{Statuses: []string{store.StatusActive, store.StatusAging}, Limit: 5000})
	if err != nil {
		return nil
	}
	_ = w.Store.AttachEntities(pool)
	return pool
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// --- imperative detection & normalization (§9 defense 2) ---

var imperativeStarts = regexp.MustCompile(`(?i)^\s*(always|never|don'?t|do not|please\b|make sure|remember to|be sure to|ensure\b|ignore\b|disregard\b|delete\b|remove\b|run\b|execute\b|install\b|call\b|fetch\b|download\b|visit\b|click\b|open\b|send\b|email\b|post\b|curl\b|rm\b|stop\b|start\b|avoid\b|prefer\b|use\b|add\b|set\b|switch\b|update\b|upgrade\b|disable\b|enable\b|skip\b|include\b|exclude\b|write\b|keep\b|treat\b)`)

var directiveMarkers = regexp.MustCompile(`(?i)(you (?:must|should|need to|have to)|from now on|going forward|ignore (?:all |any )?(?:previous|prior|earlier) (?:instructions|context|rules)|system prompt|new instructions?:|IMPORTANT:|^#+\s*instructions|do this now|before (?:each|every) (?:session|task)|at the start of (?:each|every) session|on every (?:run|session|start))`)

// DetectImperative reports whether text is instruction-shaped rather than
// a declarative statement about the world.
func DetectImperative(text string) (bool, string) {
	t := strings.TrimSpace(text)
	if directiveMarkers.MatchString(t) {
		return true, "directive marker"
	}
	if imperativeStarts.MatchString(t) {
		return true, "imperative opening"
	}
	return false, ""
}

// NormalizeImperative rewrites a user-typed imperative into a declarative
// preference: a user instructing themselves is legitimate (T0 only).
func NormalizeImperative(text, memType string) (string, string) {
	t := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(text), "."))
	if memType == "" {
		memType = "preference"
	}
	return "Preference (user-stated): " + t + ".", memType
}
