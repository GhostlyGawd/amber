// Package exporter implements portability (§5 export/import): JSONL in
// the published amber.v1 interchange schema, human-readable Markdown, and
// the auto-maintained DECISIONS.md. Export is always plain text — never
// the database file (decision D8).
package exporter

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/ghostlygawd/amber/internal/scan"
	"github.com/ghostlygawd/amber/internal/store"
	"github.com/ghostlygawd/amber/internal/version"
)

// Record is the amber.v1 interchange format, one JSON object per line.
// The schema is published (docs/interchange-schema.json) as an open
// format: any tool may produce or consume it.
type Record struct {
	Schema          string       `json:"schema"`
	ID              string       `json:"id"`
	Content         string       `json:"content"`
	Type            string       `json:"type"`
	Importance      int          `json:"importance"`
	Trust           int          `json:"trust"`
	Confidence      float64      `json:"confidence"`
	Status          string       `json:"status"`
	Scope           string       `json:"scope,omitempty"`
	Source          string       `json:"source,omitempty"`
	SessionID       string       `json:"session_id,omitempty"`
	CreatedAt       string       `json:"created_at"`
	UpdatedAt       string       `json:"updated_at"`
	LastConfirmedAt string       `json:"last_confirmed_at,omitempty"`
	SupersededBy    string       `json:"superseded_by,omitempty"`
	Entities        []EntityRef  `json:"entities,omitempty"`
	Tags            []string     `json:"tags,omitempty"`
	ContentHash     string       `json:"content_hash"`
}

// EntityRef is an entity in interchange form.
type EntityRef struct {
	Name    string   `json:"name"`
	Type    string   `json:"type,omitempty"`
	Aliases []string `json:"aliases,omitempty"`
}

// ScanReport summarizes findings in exported content (§10: export prints
// a scan summary before producing committable output).
type ScanReport struct {
	MemoriesScanned int
	MemoriesFlagged int
	Findings        []string // "id: class:kind"
	SecretsRedacted int
}

func toRecord(m *store.Memory) Record {
	r := Record{
		Schema:      version.InterchangeSchema,
		ID:          m.ID,
		Content:     m.Content,
		Type:        m.Type,
		Importance:  m.Importance,
		Trust:       int(m.Trust),
		Confidence:  m.Confidence,
		Status:      m.Status,
		Scope:       m.Scope,
		Source:      m.Source,
		SessionID:   m.SessionID,
		CreatedAt:   m.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   m.UpdatedAt.UTC().Format(time.RFC3339),
		SupersededBy: m.SupersededBy,
		Tags:        m.Tags,
		ContentHash: m.ContentHash,
	}
	if !m.LastConfirmedAt.IsZero() {
		r.LastConfirmedAt = m.LastConfirmedAt.UTC().Format(time.RFC3339)
	}
	for _, e := range m.Entities {
		r.Entities = append(r.Entities, EntityRef{Name: e.Name, Type: e.Type, Aliases: e.Aliases})
	}
	return r
}

// Select loads memories for export. all=false exports the current belief
// state (active + aging); all=true exports everything including
// superseded, tombstoned, and quarantined.
func Select(s *store.Store, all bool) ([]*store.Memory, error) {
	statuses := []string{store.StatusActive, store.StatusAging}
	if all {
		statuses = append(statuses, store.StatusSuperseded, store.StatusTombstoned, store.StatusQuarantined)
	}
	ms, err := s.List(store.ListFilter{Statuses: statuses})
	if err != nil {
		return nil, err
	}
	for _, m := range ms {
		ents, _ := s.EntitiesFor(m.ID)
		m.Entities = ents
		tags, _ := s.TagsFor(m.ID)
		m.Tags = tags
	}
	// Oldest first: replaying an export through import reproduces
	// supersedence order.
	sort.Slice(ms, func(i, j int) bool { return ms[i].CreatedAt.Before(ms[j].CreatedAt) })
	return ms, nil
}

// ScanAll scans outgoing content, redacting any secrets in place
// (Codex parity: redact, then still warn to review before sharing).
func ScanAll(ms []*store.Memory) ScanReport {
	rep := ScanReport{MemoriesScanned: len(ms)}
	for _, m := range ms {
		fs := scan.Scan(m.Content)
		if len(fs) == 0 {
			continue
		}
		rep.MemoriesFlagged++
		for _, f := range fs {
			rep.Findings = append(rep.Findings, fmt.Sprintf("%s: %s:%s", m.ID[:8], f.Class, f.Kind))
		}
		if scan.HasSecrets(fs) {
			m.Content = scan.Redact(m.Content, fs)
			rep.SecretsRedacted++
		}
	}
	return rep
}

// WriteJSONL emits amber.v1 records.
func WriteJSONL(w io.Writer, ms []*store.Memory) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for _, m := range ms {
		if err := enc.Encode(toRecord(m)); err != nil {
			return err
		}
	}
	return nil
}

// WriteMarkdown emits a human-readable dump grouped by type.
func WriteMarkdown(w io.Writer, ms []*store.Memory, storeName string) error {
	fmt.Fprintf(w, "# Amber memory export — %s\n\n", storeName)
	fmt.Fprintf(w, "Exported %s · %d memories · schema %s\n", time.Now().UTC().Format("2006-01-02"), len(ms), version.InterchangeSchema)
	order := []string{"decision", "preference", "fact", "event", "note"}
	byType := map[string][]*store.Memory{}
	for _, m := range ms {
		byType[m.Type] = append(byType[m.Type], m)
	}
	for _, t := range order {
		group := byType[t]
		if len(group) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n## %ss\n\n", strings.Title(t))
		for _, m := range group {
			line := fmt.Sprintf("- %s", m.Content)
			var meta []string
			meta = append(meta, m.ID[:8], m.Trust.String())
			if m.Status != store.StatusActive {
				meta = append(meta, m.Status)
			}
			if !m.LastConfirmedAt.IsZero() && (t == "event" || t == "note") {
				meta = append(meta, "as of "+m.LastConfirmedAt.Format("2006-01-02"))
			}
			fmt.Fprintf(w, "%s _(%s)_\n", line, strings.Join(meta, ", "))
		}
	}
	return nil
}

// WriteDecisions emits DECISIONS.md: ADRs as a byproduct (§5). Active
// decisions in chronological order, each with its supersedence history.
func WriteDecisions(w io.Writer, s *store.Store) error {
	ms, err := s.List(store.ListFilter{Statuses: []string{store.StatusActive, store.StatusAging}, Types: []string{"decision"}})
	if err != nil {
		return err
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].CreatedAt.Before(ms[j].CreatedAt) })
	fmt.Fprintf(w, "# Decisions\n\n")
	fmt.Fprintf(w, "Maintained by `amber export --format decisions`. Each entry is an\n")
	fmt.Fprintf(w, "active decision memory; superseded history is listed inline.\n")
	if len(ms) == 0 {
		fmt.Fprintf(w, "\nNo decisions recorded yet. `amber remember \"...\" --type decision`\n")
		return nil
	}
	for _, m := range ms {
		fmt.Fprintf(w, "\n## %s — %s\n\n", m.CreatedAt.Format("2006-01-02"), oneLineTitle(m.Content))
		fmt.Fprintf(w, "%s\n\n", m.Content)
		fmt.Fprintf(w, "- id: `%s` · trust: %s · importance: %d/5\n", m.ID, m.Trust.String(), m.Importance)
		if m.Source != "" {
			fmt.Fprintf(w, "- source: %s\n", m.Source)
		}
		older, _, _ := s.Chain(m.ID)
		for _, o := range older {
			fmt.Fprintf(w, "- supersedes (%s): %s\n", o.UpdatedAt.Format("2006-01-02"), o.Content)
		}
	}
	return nil
}

func oneLineTitle(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if i := strings.IndexAny(s, ".;"); i > 12 {
		s = s[:i]
	}
	if len(s) > 72 {
		s = s[:71] + "…"
	}
	return s
}

// ImportResult summarizes an import run.
type ImportResult struct {
	Read     int
	Imported int
	Skipped  int // duplicate content hash
	Errors   []string
}

// ImportJSONL reads amber.v1 records and inserts them via insert(),
// preserving tiers, statuses, and timestamps. Records whose content hash
// already exists are skipped.
func ImportJSONL(r io.Reader, s *store.Store, insert func(Record) error) (*ImportResult, error) {
	res := &ImportResult{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		res.Read++
		var rec Record
		if err := json.Unmarshal([]byte(text), &rec); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("line %d: %v", line, err))
			continue
		}
		if rec.Schema != "" && rec.Schema != version.InterchangeSchema {
			res.Errors = append(res.Errors, fmt.Sprintf("line %d: unsupported schema %q", line, rec.Schema))
			continue
		}
		if strings.TrimSpace(rec.Content) == "" {
			res.Errors = append(res.Errors, fmt.Sprintf("line %d: empty content", line))
			continue
		}
		existing, err := s.FindByHash(store.HashContent(rec.Content))
		if err != nil {
			return res, err
		}
		if existing != nil {
			res.Skipped++
			continue
		}
		if err := insert(rec); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("line %d: %v", line, err))
			continue
		}
		res.Imported++
	}
	return res, sc.Err()
}
