// Package belief implements the belief-state semantics of §8: on every
// write, decide whether the new claim is a duplicate (reconfirm), a
// contradiction (supersede), or ambiguous (keep both, flag for review).
// Everything is soft and reversible; nothing is ever hard-deleted.
package belief

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ghostlygawd/amber/internal/embed"
	"github.com/ghostlygawd/amber/internal/store"
)

// Verdict of the write-time adjudication.
type Verdict string

const (
	VerdictNew        Verdict = "new"
	VerdictDuplicate  Verdict = "duplicate"
	VerdictSupersedes Verdict = "supersedes"
	VerdictAmbiguous  Verdict = "ambiguous"
)

// Decision describes what the write pipeline should do.
type Decision struct {
	Verdict Verdict
	// Existing is the memory this decision is about (duplicate of /
	// superseded by the new content / ambiguous with).
	Existing *store.Memory
	// Similarity that triggered the decision.
	Similarity float64
	Reason     string
}

// Thresholds (tuned by the contradiction suite; see testdata/).
//
// Two contradiction floors: when the token-level subject overlap is
// strong (≥50% of the smaller claim) the claims demonstrably discuss the
// same thing and a lower overall similarity suffices; when the only link
// is a shared entity, require high similarity — people accrete many
// unrelated true facts, and a low bar would let "Alice is the TL" retire
// "Alice is on vacation".
const (
	dupThreshold       = 0.93
	contraEntityFloor  = 0.55
	contraSubjectFloor = 0.38
	ambiguousThreshold = 0.82
)

// Adjudicate compares new content against candidate actives and returns a
// decision. entities are the entity names linked to the new content.
// newVec may be nil (BM25-only floor) — token overlap is used instead.
func Adjudicate(content string, newVec []float32, entities []string, candidates []*store.Memory) Decision {
	newHash := store.HashContent(content)
	newToks := tokenSet(content)
	entSet := map[string]bool{}
	for _, e := range entities {
		entSet[strings.ToLower(e)] = true
	}

	best := Decision{Verdict: VerdictNew}
	type scored struct {
		m   *store.Memory
		sim float64
	}
	var ranked []scored
	for _, c := range candidates {
		if c.Status != store.StatusActive && c.Status != store.StatusAging {
			continue
		}
		if c.ContentHash == newHash {
			return Decision{Verdict: VerdictDuplicate, Existing: c, Similarity: 1, Reason: "identical normalized content"}
		}
		sim := similarity(newVec, c, newToks)
		ranked = append(ranked, scored{c, sim})
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].sim > ranked[j].sim })

	for _, rc := range ranked {
		c, sim := rc.m, rc.sim
		if sim >= dupThreshold {
			return Decision{Verdict: VerdictDuplicate, Existing: c, Similarity: sim, Reason: fmt.Sprintf("near-duplicate (similarity %.2f)", sim)}
		}
		if sim < contraSubjectFloor {
			break // ranked descending; nothing below can qualify
		}
		strongSubject := subjectOverlap(newToks, c)
		sharedEntity := sharesEntity(entSet, c)
		floor := contraEntityFloor
		if strongSubject {
			floor = contraSubjectFloor
		}
		if (!strongSubject && !sharedEntity) || sim < floor {
			continue
		}
		if incompatible(content, c.Content) {
			return Decision{Verdict: VerdictSupersedes, Existing: c, Similarity: sim,
				Reason: fmt.Sprintf("same subject, incompatible claim (similarity %.2f)", sim)}
		}
		if sim >= ambiguousThreshold && best.Verdict == VerdictNew {
			best = Decision{Verdict: VerdictAmbiguous, Existing: c, Similarity: sim,
				Reason: fmt.Sprintf("similar claim, cannot determine contradiction (similarity %.2f)", sim)}
		}
	}
	return best
}

func similarity(newVec []float32, c *store.Memory, newToks map[string]bool) float64 {
	if len(newVec) > 0 && len(c.Embedding) == len(newVec) {
		return embed.Cosine(newVec, c.Embedding)
	}
	return jaccard(newToks, tokenSet(c.Content))
}

func sharesEntity(entSet map[string]bool, c *store.Memory) bool {
	for _, e := range c.Entities {
		if entSet[strings.ToLower(e.Name)] {
			return true
		}
		for _, a := range e.Aliases {
			if entSet[strings.ToLower(a)] {
				return true
			}
		}
	}
	return false
}

// subjectOverlap: contentful-token overlap approximates "same subject"
// for entity-less claims ("The staging DB is X" vs "The staging DB is Y").
// Filler words are excluded so "the billing service" cannot fake a
// subject match between unrelated claims.
func subjectOverlap(newToks map[string]bool, c *store.Memory) bool {
	oldToks := tokenSet(c.Content)
	shared, newN, oldN := 0, 0, 0
	for t := range newToks {
		if fillerWords[t] {
			continue
		}
		newN++
		if oldToks[t] {
			shared++
		}
	}
	for t := range oldToks {
		if !fillerWords[t] {
			oldN++
		}
	}
	minLen := newN
	if oldN < minLen {
		minLen = oldN
	}
	if minLen == 0 {
		return false
	}
	return float64(shared)/float64(minLen) >= 0.5
}

// --- incompatibility heuristics ("low entailment" approximation) ---

var negationRe = regexp.MustCompile(`(?i)\b(not|no longer|never|stopped|isn't|aren't|doesn't|don't|won't|wasn't|weren't|dropped|removed|deprecated|retired|replaced|instead of|rather than|switched (?:from|away|to)|moved (?:off|away)|migrated (?:from|off))\b`)

var relationVerbs = regexp.MustCompile(`(?i)\b(prefers?|uses?|using|is|are|was|were|works? (?:at|on|with)|lives? in|based in|named|called|set to|runs? (?:on|at|in)|deploys?|ships?|expires?|go(?:es)? out|happens?|scheduled|stores?|chose|chosen|decided|decision|targets?|requires?|defaults? to|written in|hosted on|will|should be)\b`)

var valueTokenRe = regexp.MustCompile(`^[a-z0-9][a-z0-9.\-_/+]*$`)

// incompatible reports whether two same-subject claims cannot both hold.
func incompatible(a, b string) bool {
	negA := negationRe.MatchString(a)
	negB := negationRe.MatchString(b)
	if negA != negB {
		// One asserts, the other denies — near-certain contradiction when
		// subjects already matched.
		return true
	}
	// Both claims must share a relation verb: "X deploys on Fridays" and
	// "X is the TL" describe different relations of the same subject and
	// can both hold.
	if !sharedRelationVerb(a, b) {
		return false
	}
	// Same relation skeleton, different values: strip shared tokens; if
	// both sides retain distinct value-like tokens (numbers, identifiers,
	// proper nouns), the claims disagree on the value.
	ta, tb := tokenSet(a), tokenSet(b)
	restA := diffValues(ta, tb)
	restB := diffValues(tb, ta)
	return len(restA) > 0 && len(restB) > 0
}

func sharedRelationVerb(a, b string) bool {
	va := relationVerbs.FindAllString(strings.ToLower(a), -1)
	vb := relationVerbs.FindAllString(strings.ToLower(b), -1)
	if len(va) == 0 || len(vb) == 0 {
		return false
	}
	set := map[string]bool{}
	for _, v := range va {
		set[verbLemma(v)] = true
	}
	for _, v := range vb {
		if set[verbLemma(v)] {
			return true
		}
	}
	return false
}

// verbLemma folds trivial inflection so "prefer/prefers", "run on/runs
// on" and "expire/expires" compare equal.
func verbLemma(v string) string {
	v = strings.TrimSpace(v)
	fields := strings.Fields(v)
	if len(fields) > 0 {
		fields[0] = strings.TrimSuffix(strings.TrimSuffix(fields[0], "es"), "s")
		v = strings.Join(fields, " ")
	}
	return v
}

func diffValues(a, b map[string]bool) []string {
	var out []string
	for t := range a {
		if b[t] || fillerWords[t] {
			continue
		}
		if valueTokenRe.MatchString(t) || hasDigit(t) {
			out = append(out, t)
		}
	}
	return out
}

var fillerWords = map[string]bool{
	"the": true, "a": true, "an": true, "to": true, "of": true, "for": true, "in": true,
	"on": true, "at": true, "and": true, "or": true, "with": true, "now": true, "still": true,
	"currently": true, "always": true, "usually": true, "we": true, "user": true, "team": true,
	"project": true, "prefers": true, "prefer": true, "uses": true, "use": true, "using": true,
	"is": true, "are": true, "was": true, "were": true, "be": true, "been": true, "will": true,
	"should": true, "it": true, "this": true, "that": true, "as": true, "by": true, "from": true,
	"decided": true, "decision": true, "set": true, "named": true, "called": true, "new": true,
}

func hasDigit(s string) bool {
	for _, c := range s {
		if c >= '0' && c <= '9' {
			return true
		}
	}
	return false
}

func tokenSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, t := range strings.Fields(store.NormalizeContent(s)) {
		out[t] = true
	}
	return out
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for t := range a {
		if b[t] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// --- aging (§8) ---

// HalfLife returns the per-type confidence half-life. Preferences and
// decisions do not decay — they supersede. Zero means no decay.
func HalfLife(memType string) time.Duration {
	switch memType {
	case "event":
		return 90 * 24 * time.Hour
	case "note":
		return 180 * 24 * time.Hour
	case "fact":
		return 540 * 24 * time.Hour
	default: // preference, decision
		return 0
	}
}

// AgingThreshold: below this effective confidence a memory ages out of
// auto-injection (still recallable, never deleted).
const AgingThreshold = 0.35

// EffectiveConfidence decays stored confidence by time since last
// confirmation, per type.
func EffectiveConfidence(m *store.Memory, now time.Time) float64 {
	hl := HalfLife(m.Type)
	if hl == 0 {
		return m.Confidence
	}
	anchor := m.LastConfirmedAt
	if anchor.IsZero() {
		anchor = m.UpdatedAt
	}
	if anchor.IsZero() {
		anchor = m.CreatedAt
	}
	age := now.Sub(anchor)
	if age <= 0 {
		return m.Confidence
	}
	halves := float64(age) / float64(hl)
	return m.Confidence * math.Exp2(-halves)
}
