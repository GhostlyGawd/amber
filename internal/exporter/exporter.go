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
	"github.com/ghostlygawd/amber/internal/trust"
	"github.com/ghostlygawd/amber/internal/version"
)

// Record is the amber.v1 interchange format, one JSON object per line.
// The schema is published (docs/interchange-schema.json) as an open
// format: any tool may produce or consume it.
type Record struct {
	Schema          string      `json:"schema"`
	ID              string      `json:"id"`
	Content         string      `json:"content"`
	Type            string      `json:"type"`
	Importance      int         `json:"importance"`
	Trust           int         `json:"trust"`
	Confidence      float64     `json:"confidence"`
	Status          string      `json:"status"`
	Scope           string      `json:"scope,omitempty"`
	Source          string      `json:"source,omitempty"`
	SessionID       string      `json:"session_id,omitempty"`
	CreatedAt       string      `json:"created_at"`
	UpdatedAt       string      `json:"updated_at"`
	LastConfirmedAt string      `json:"last_confirmed_at,omitempty"`
	SupersededBy    string      `json:"superseded_by,omitempty"`
	Entities        []EntityRef `json:"entities,omitempty"`
	Tags            []string    `json:"tags,omitempty"`
	ContentHash     string      `json:"content_hash"`
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
		Schema:       version.InterchangeSchema,
		ID:           m.ID,
		Content:      m.Content,
		Type:         m.Type,
		Importance:   m.Importance,
		Trust:        int(m.Trust),
		Confidence:   m.Confidence,
		Status:       m.Status,
		Scope:        m.Scope,
		Source:       m.Source,
		SessionID:    m.SessionID,
		CreatedAt:    m.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:    m.UpdatedAt.UTC().Format(time.RFC3339),
		SupersededBy: m.SupersededBy,
		Tags:         m.Tags,
		ContentHash:  store.HashContent(m.Content),
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

// ImportJSONL reads and validates a complete amber.v1 stream, then imports it
// atomically. IDs, tiers, statuses, timestamps, supersedence, aliases, and tags
// are preserved. Records whose content hash already exists are skipped.
func ImportJSONL(r io.Reader, s *store.Store) (*ImportResult, error) {
	res := &ImportResult{}
	type pendingImport struct {
		record   Record
		line     int
		memory   *store.Memory
		entities []store.Entity
	}
	var pending []pendingImport
	idMap := make(map[string]string)
	seenHashes := make(map[string]string)
	usedIDs := make(map[string]bool)

	seenOriginalIDs := make(map[string]bool)
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
		if rec.Schema != version.InterchangeSchema {
			res.Errors = append(res.Errors, fmt.Sprintf("line %d: unsupported schema %q", line, rec.Schema))
			continue
		}
		if strings.TrimSpace(rec.ID) == "" {
			res.Errors = append(res.Errors, fmt.Sprintf("line %d: empty id", line))
			continue
		}
		if seenOriginalIDs[rec.ID] {
			res.Errors = append(res.Errors, fmt.Sprintf("line %d: duplicate id %q", line, rec.ID))
			continue
		}
		seenOriginalIDs[rec.ID] = true
		if strings.TrimSpace(rec.Content) == "" {
			res.Errors = append(res.Errors, fmt.Sprintf("line %d: empty content", line))
			continue
		}
		if !store.ValidType(rec.Type) {
			res.Errors = append(res.Errors, fmt.Sprintf("line %d: bad type %q", line, rec.Type))
			continue
		}
		tier := trust.Tier(rec.Trust)
		if !tier.Valid() {
			res.Errors = append(res.Errors, fmt.Sprintf("line %d: bad trust tier %d", line, rec.Trust))
			continue
		}
		if rec.Scope != "" && rec.Scope != "global" && rec.Scope != "project" {
			res.Errors = append(res.Errors, fmt.Sprintf("line %d: bad scope %q", line, rec.Scope))
			continue
		}
		if !validImportStatus(rec.Status) {
			res.Errors = append(res.Errors, fmt.Sprintf("line %d: bad status %q", line, rec.Status))
			continue
		}
		created, err := parseImportTime(rec.CreatedAt, true)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("line %d: created_at: %v", line, err))
			continue
		}
		updated, err := parseImportTime(rec.UpdatedAt, true)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("line %d: updated_at: %v", line, err))
			continue
		}
		confirmed, err := parseImportTime(rec.LastConfirmedAt, false)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("line %d: last_confirmed_at: %v", line, err))
			continue
		}
		entities := make([]store.Entity, 0, len(rec.Entities))
		badEntity := false
		for _, entity := range rec.Entities {
			entity.Name = strings.TrimSpace(entity.Name)
			if entity.Type == "" {
				entity.Type = "other"
			}
			if entity.Name == "" || (entity.Type != "person" && entity.Type != "project" && entity.Type != "org" && entity.Type != "other") {
				res.Errors = append(res.Errors, fmt.Sprintf("line %d: invalid entity name or type", line))
				badEntity = true
				break
			}
			entities = append(entities, store.Entity{Name: entity.Name, Type: entity.Type, Aliases: entity.Aliases})
		}
		if badEntity {
			continue
		}
		hash := store.HashContent(rec.Content)
		if rec.ContentHash != hash {
			res.Errors = append(res.Errors, fmt.Sprintf("line %d: content_hash does not match content", line))
			continue
		}
		if actual, ok := seenHashes[hash]; ok {
			idMap[rec.ID] = actual
			res.Skipped++
			continue
		}
		existing, err := s.FindByHash(hash)
		if err != nil {
			return res, err
		}
		if existing != nil {
			idMap[rec.ID] = existing.ID
			seenHashes[hash] = existing.ID
			res.Skipped++
			continue
		}
		actualID := rec.ID
		var collision int
		if err := s.DB.QueryRow(`SELECT COUNT(*) FROM memories WHERE id=?`, actualID).Scan(&collision); err != nil {
			return res, err
		}
		if collision > 0 || usedIDs[actualID] {
			actualID = store.NewID()
		}
		usedIDs[actualID] = true
		idMap[rec.ID] = actualID
		seenHashes[hash] = actualID
		status := rec.Status
		if tier == trust.T3 && (status == store.StatusActive || status == store.StatusAging) {
			status = store.StatusQuarantined
		}
		importance := rec.Importance
		if importance == 0 {
			importance = 3
		}
		if importance < 1 || importance > 5 || rec.Confidence < 0 || rec.Confidence > 1 {
			res.Errors = append(res.Errors, fmt.Sprintf("line %d: importance or confidence out of range", line))
			continue
		}
		m := &store.Memory{
			ID: actualID, Content: rec.Content, Type: rec.Type, Importance: importance,
			Trust: tier, Confidence: rec.Confidence, Status: status, Scope: rec.Scope,
			Source: rec.Source, SessionID: rec.SessionID, CreatedAt: created, UpdatedAt: updated,
			LastConfirmedAt: confirmed, ContentHash: hash,
		}
		pending = append(pending, pendingImport{record: rec, line: line, memory: m, entities: entities})
	}
	if err := sc.Err(); err != nil {
		return res, err
	}

	items := make([]store.ImportItem, 0, len(pending))
	for _, item := range pending {
		if item.record.SupersededBy != "" {
			target, ok := idMap[item.record.SupersededBy]
			if !ok {
				var exists int
				if err := s.DB.QueryRow(`SELECT COUNT(*) FROM memories WHERE id=?`, item.record.SupersededBy).Scan(&exists); err != nil {
					return res, err
				}
				if exists == 0 {
					res.Errors = append(res.Errors, fmt.Sprintf("line %d: superseded_by references unknown id %q", item.line, item.record.SupersededBy))
					continue
				}
				target = item.record.SupersededBy
			}
			item.memory.SupersededBy = target
		}
		items = append(items, store.ImportItem{Memory: item.memory, Entities: item.entities, Tags: item.record.Tags, OriginalID: item.record.ID})
	}
	if len(res.Errors) > 0 {
		return res, nil
	}
	if err := s.ImportBatch(items); err != nil {
		return res, err
	}
	res.Imported = len(items)
	return res, nil
}

func validImportStatus(status string) bool {
	switch status {
	case store.StatusActive, store.StatusAging, store.StatusQuarantined, store.StatusSuperseded, store.StatusTombstoned:
		return true
	default:
		return false
	}
}

func parseImportTime(value string, required bool) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		if required {
			return time.Time{}, fmt.Errorf("required value is empty")
		}
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed, nil
}
