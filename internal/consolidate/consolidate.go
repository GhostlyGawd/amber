// Package consolidate implements the background maintenance pass
// (decision D16): merge duplicates, resolve contradictions via
// supersedence, absolutize relative dates, demote aged memories, and
// re-index.
//
// Positioning stance, enforced in code: consolidation NEVER deletes.
// Every action is a status transition or content rewrite journaled to ops
// with enough payload to reverse it.
package consolidate

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ghostlygawd/amber/internal/belief"
	"github.com/ghostlygawd/amber/internal/embed"
	"github.com/ghostlygawd/amber/internal/store"
)

// Action is one planned/performed consolidation step.
type Action struct {
	Kind   string `json:"kind"` // merge|supersede|absolutize|demote|reindex
	Memory string `json:"memory_id"`
	Target string `json:"target_id,omitempty"`
	Detail string `json:"detail"`
}

// Report of a consolidation pass.
type Report struct {
	Actions   []Action `json:"actions"`
	Merged    int      `json:"merged"`
	Resolved  int      `json:"resolved"`
	Dated     int      `json:"dated"`
	Demoted   int      `json:"demoted"`
	Reindexed int      `json:"reindexed"`
	DryRun    bool     `json:"dry_run"`
}

// Run executes (or plans, with dryRun) a consolidation pass over actives
// updated since `since` (zero = all).
func Run(s *store.Store, e embed.Embedder, since time.Time, dryRun bool) (*Report, error) {
	rep := &Report{DryRun: dryRun}
	now := time.Now().UTC()

	pool, err := s.List(store.ListFilter{Statuses: []string{store.StatusActive, store.StatusAging}, Since: since})
	if err != nil {
		return nil, err
	}
	_ = s.AttachEntities(pool)

	// 1+2. Pairwise duplicate merge and contradiction resolution. O(n²)
	// over the (bounded) active set; newest first so the newest claim wins.
	retired := map[string]bool{}
	for i := 0; i < len(pool); i++ {
		a := pool[i]
		if retired[a.ID] {
			continue
		}
		for j := i + 1; j < len(pool); j++ {
			b := pool[j] // older than a (List sorts newest first)
			if retired[b.ID] {
				continue
			}
			var entNames []string
			for _, en := range a.Entities {
				entNames = append(entNames, en.Name)
			}
			dec := belief.Adjudicate(a.Content, a.Embedding, entNames, []*store.Memory{b})
			switch dec.Verdict {
			case belief.VerdictDuplicate:
				rep.Merged++
				rep.Actions = append(rep.Actions, Action{Kind: "merge", Memory: b.ID, Target: a.ID,
					Detail: fmt.Sprintf("duplicate of %s (%s)", a.ID[:8], dec.Reason)})
				if !dryRun {
					if err := s.Supersede(b.ID, a.ID); err != nil {
						return rep, err
					}
					if err := s.Reconfirm(a.ID, b.Importance); err != nil {
						return rep, err
					}
				}
				retired[b.ID] = true
			case belief.VerdictSupersedes:
				rep.Resolved++
				rep.Actions = append(rep.Actions, Action{Kind: "supersede", Memory: b.ID, Target: a.ID,
					Detail: fmt.Sprintf("contradicted by newer %s (%s)", a.ID[:8], dec.Reason)})
				if !dryRun {
					if err := s.Supersede(b.ID, a.ID); err != nil {
						return rep, err
					}
				}
				retired[b.ID] = true
			}
		}
	}

	// 3. Absolutize relative dates against created_at (originals kept in
	// the ops payload — reversible).
	for _, m := range pool {
		if retired[m.ID] {
			continue
		}
		rewritten, changed := AbsolutizeDates(m.Content, m.CreatedAt)
		if !changed {
			continue
		}
		rep.Dated++
		rep.Actions = append(rep.Actions, Action{Kind: "absolutize", Memory: m.ID,
			Detail: fmt.Sprintf("%q → %q", oneLine(m.Content), oneLine(rewritten))})
		if !dryRun {
			if err := s.UpdateContent(m.ID, rewritten, "consolidate: absolutize relative date"); err != nil {
				return rep, err
			}
			if e != nil {
				if v, err := e.Embed(rewritten); err == nil {
					_ = s.SetEmbedding(m.ID, v)
				}
			}
		}
	}

	// 4. Demote aged memories: below the confidence threshold they leave
	// auto-injection but stay recallable. Never deleted.
	for _, m := range pool {
		if retired[m.ID] || m.Status != store.StatusActive {
			continue
		}
		if belief.EffectiveConfidence(m, now) < belief.AgingThreshold {
			rep.Demoted++
			rep.Actions = append(rep.Actions, Action{Kind: "demote", Memory: m.ID,
				Detail: fmt.Sprintf("effective confidence %.2f < %.2f", belief.EffectiveConfidence(m, now), belief.AgingThreshold)})
			if !dryRun {
				if err := s.SetStatus(m.ID, store.StatusAging, store.OpAge, map[string]any{"via": "consolidate"}); err != nil {
					return rep, err
				}
			}
		}
	}

	// 5. Re-index: fill missing embeddings (e.g. rows written under the
	// BM25 floor after the model arrived).
	if e != nil && !dryRun {
		ms, err := s.List(store.ListFilter{Statuses: []string{store.StatusActive, store.StatusAging}})
		if err != nil {
			return rep, err
		}
		for _, m := range ms {
			if len(m.Embedding) != 0 {
				continue
			}
			v, err := e.Embed(m.Content)
			if err != nil {
				continue
			}
			if err := s.SetEmbedding(m.ID, v); err != nil {
				return rep, err
			}
			rep.Reindexed++
		}
		if rep.Reindexed > 0 {
			rep.Actions = append(rep.Actions, Action{Kind: "reindex", Detail: fmt.Sprintf("embedded %d rows", rep.Reindexed)})
		}
	}

	if !dryRun {
		_ = s.AppendOp(store.OpConsolidate, "", map[string]any{
			"merged": rep.Merged, "resolved": rep.Resolved, "dated": rep.Dated,
			"demoted": rep.Demoted, "reindexed": rep.Reindexed,
		})
	}
	return rep, nil
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 60 {
		return s[:59] + "…"
	}
	return s
}

// --- relative-date absolutization ---

var weekdays = map[string]time.Weekday{
	"sunday": time.Sunday, "monday": time.Monday, "tuesday": time.Tuesday,
	"wednesday": time.Wednesday, "thursday": time.Thursday, "friday": time.Friday,
	"saturday": time.Saturday,
}

var relDateRe = regexp.MustCompile(`(?i)\b(yesterday|today|tomorrow|last (?:sunday|monday|tuesday|wednesday|thursday|friday|saturday|week|month)|next (?:sunday|monday|tuesday|wednesday|thursday|friday|saturday|week|month)|(\d+) days? ago|this (?:morning|afternoon|evening|week|month))\b`)

// AbsolutizeDates rewrites relative date phrases using anchor (the
// memory's creation time) as "now". Only unambiguous phrases are
// rewritten; vaguer ones ("this week") get an as-of annotation instead.
func AbsolutizeDates(content string, anchor time.Time) (string, bool) {
	if anchor.IsZero() {
		return content, false
	}
	changed := false
	out := relDateRe.ReplaceAllStringFunc(content, func(match string) string {
		lower := strings.ToLower(match)
		var t time.Time
		switch {
		case lower == "yesterday":
			t = anchor.AddDate(0, 0, -1)
		case lower == "today", lower == "this morning", lower == "this afternoon", lower == "this evening":
			t = anchor
		case lower == "tomorrow":
			t = anchor.AddDate(0, 0, 1)
		case strings.HasPrefix(lower, "last "):
			rest := strings.TrimPrefix(lower, "last ")
			if wd, ok := weekdays[rest]; ok {
				t = prevWeekday(anchor, wd)
			} else if rest == "week" {
				return fmt.Sprintf("the week of %s", anchor.AddDate(0, 0, -7).Format("2006-01-02"))
			} else if rest == "month" {
				return anchor.AddDate(0, -1, 0).Format("January 2006")
			}
		case strings.HasPrefix(lower, "next "):
			rest := strings.TrimPrefix(lower, "next ")
			if wd, ok := weekdays[rest]; ok {
				t = nextWeekday(anchor, wd)
			} else if rest == "week" {
				return fmt.Sprintf("the week of %s", anchor.AddDate(0, 0, 7).Format("2006-01-02"))
			} else if rest == "month" {
				return anchor.AddDate(0, 1, 0).Format("January 2006")
			}
		case strings.HasSuffix(lower, "days ago"), strings.HasSuffix(lower, "day ago"):
			sub := relDateRe.FindStringSubmatch(match)
			if len(sub) > 2 && sub[2] != "" {
				if n, err := strconv.Atoi(sub[2]); err == nil {
					t = anchor.AddDate(0, 0, -n)
				}
			}
		case lower == "this week":
			return fmt.Sprintf("the week of %s", anchor.Format("2006-01-02"))
		case lower == "this month":
			return anchor.Format("January 2006")
		}
		if t.IsZero() {
			return match
		}
		changed = true
		return "on " + t.Format("2006-01-02")
	})
	// "on on 2026-.." cleanup when the source already said "on yesterday" etc.
	out = strings.ReplaceAll(out, "on on ", "on ")
	if !changed && out != content {
		changed = true
	}
	return out, changed
}

func prevWeekday(from time.Time, wd time.Weekday) time.Time {
	d := int(from.Weekday()) - int(wd)
	if d <= 0 {
		d += 7
	}
	return from.AddDate(0, 0, -d)
}

func nextWeekday(from time.Time, wd time.Weekday) time.Time {
	d := int(wd) - int(from.Weekday())
	if d <= 0 {
		d += 7
	}
	return from.AddDate(0, 0, d)
}
