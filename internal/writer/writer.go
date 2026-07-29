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
	aliasName, aliasEmail := entityx.EmailAlias(content)
	if aliasName != "" {
		entNames[aliasName] = "person"
	}
	ents := make([]store.Entity, 0, len(entNames))
	for n, t := range entNames {
		e := store.Entity{Name: n, Type: t}
		if strings.EqualFold(n, aliasName) && aliasEmail != "" {
			e.Aliases = []string{aliasEmail}
		}
		ents = append(ents, e)
	}
	entNameList := make([]string, 0, len(ents))
	for _, e := range ents {
		entNameList = append(entNameList, e.Name)
	}

	// 4. Embed (nil embedder = lexical floor; belief falls back to token
	// overlap). The atomic write pins the model identity with the mutation.
	var vec []float32
	if w.Embedder != nil && !in.Quarantine && in.Trust != trust.T3 {
		v, err := w.Embedder.Embed(content)
		if err == nil {
			vec = v
		}
	}

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
	reason := in.QuarantineReason
	if status == store.StatusQuarantined {
		if reason == "" {
			reason = "untrusted origin (trust tier T3)"
		}
	}
	req := store.AtomicWriteRequest{
		Memory: m, Entities: ents, Tags: in.Tags,
		QuarantineReason: reason, QuarantineFlagKind: in.QuarantineFlagKind,
	}
	if w.Embedder != nil && len(vec) > 0 {
		req.EmbeddingModel = w.Embedder.Name()
		req.EmbeddingDims = w.Embedder.Dims()
	}
	if !in.Quarantine && in.Status == "" {
		req.Decide = func(candidates []*store.Memory) store.WriteDecision {
			decision := belief.Adjudicate(content, vec, entNameList, candidates)
			result := store.WriteDecision{Kind: store.WriteNew}
			switch decision.Verdict {
			case belief.VerdictDuplicate:
				result.Kind = store.WriteDuplicate
			case belief.VerdictSupersedes:
				result.Kind = store.WriteSupersedes
			case belief.VerdictAmbiguous:
				result.Kind = store.WriteAmbiguous
				result.FlagDetail = fmt.Sprintf("possibly contradicts %s: %q — %s",
					decision.Existing.ID, truncate(decision.Existing.Content, 120), decision.Reason)
			}
			if decision.Existing != nil {
				result.ExistingID = decision.Existing.ID
			}
			return result
		}
	}
	result, err := w.Store.AtomicWrite(req)
	if err != nil {
		return nil, err
	}
	out.Action = result.Action
	out.Memory = result.Memory
	if result.Action == "superseded" {
		out.Superseded = result.Existing
	}
	out.Ambiguous = result.Ambiguous
	return out, nil
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
