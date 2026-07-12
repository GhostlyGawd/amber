package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ghostlygawd/amber/internal/trust"
)

// ErrNotFound is returned when an id or prefix matches nothing.
var ErrNotFound = errors.New("not found")

// ErrAmbiguousID is returned when an id prefix matches multiple memories.
var ErrAmbiguousID = errors.New("ambiguous id prefix")

const memCols = `id, content, type, importance, trust, confidence, last_confirmed_at,
	scope, source, session_id, created_at, updated_at, status, superseded_by, embedding, content_hash`

func scanMemory(row interface{ Scan(...any) error }) (*Memory, error) {
	var m Memory
	var lastConfirmed, created, updated string
	var emb []byte
	var tr int
	err := row.Scan(&m.ID, &m.Content, &m.Type, &m.Importance, &tr, &m.Confidence, &lastConfirmed,
		&m.Scope, &m.Source, &m.SessionID, &created, &updated, &m.Status, &m.SupersededBy, &emb, &m.ContentHash)
	if err != nil {
		return nil, err
	}
	m.Trust = tier(tr)
	m.LastConfirmedAt = parseTime(lastConfirmed)
	m.CreatedAt = parseTime(created)
	m.UpdatedAt = parseTime(updated)
	m.Embedding = DecodeVector(emb)
	return &m, nil
}

// Insert writes a new memory row, its FTS entry, links, and a journal op,
// inside one immediate transaction.
func (s *Store) Insert(m *Memory, entities []Entity, tags []string) error {
	if m.ID == "" {
		m.ID = NewID()
	}
	now := time.Now().UTC()
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}
	if m.UpdatedAt.IsZero() {
		m.UpdatedAt = now
	}
	if m.LastConfirmedAt.IsZero() {
		m.LastConfirmedAt = now
	}
	if m.ContentHash == "" {
		m.ContentHash = HashContent(m.Content)
	}
	if m.Status == "" {
		m.Status = StatusActive
	}
	if m.Importance == 0 {
		m.Importance = 3
	}
	if m.Type == "" {
		m.Type = "note"
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`INSERT INTO memories(`+memCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.ID, m.Content, m.Type, m.Importance, int(m.Trust), m.Confidence, fmtTime(m.LastConfirmedAt),
		m.Scope, m.Source, m.SessionID, fmtTime(m.CreatedAt), fmtTime(m.UpdatedAt), m.Status,
		m.SupersededBy, EncodeVector(m.Embedding), m.ContentHash)
	if err != nil {
		return fmt.Errorf("insert memory: %w", err)
	}
	if err := ftsInsertTx(tx, m.ID, m.Content); err != nil {
		return err
	}
	if err := linkEntitiesTx(tx, m.ID, entities); err != nil {
		return err
	}
	if err := linkTagsTx(tx, m.ID, tags); err != nil {
		return err
	}
	if err := appendOpTx(tx, "create", m.ID, map[string]any{
		"content": m.Content, "type": m.Type, "trust": int(m.Trust), "status": m.Status, "source": m.Source,
	}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	m.Entities = entities
	m.Tags = tags
	return nil
}

// fts rowid mapping: we keep a shadow rowid via the memories rowid.
// memories_fts is contentless (content=''), so we manage rowids manually
// using the memories table's rowid.

func memRowidTx(tx *sql.Tx, id string) (int64, error) {
	var rid int64
	err := tx.QueryRow(`SELECT rowid FROM memories WHERE id=?`, id).Scan(&rid)
	return rid, err
}

func ftsInsertTx(tx *sql.Tx, id, content string) error {
	rid, err := memRowidTx(tx, id)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO memories_fts(rowid, content) VALUES(?,?)`, rid, content)
	return err
}

func ftsDeleteTx(tx *sql.Tx, id, oldContent string) error {
	rid, err := memRowidTx(tx, id)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO memories_fts(memories_fts, rowid, content) VALUES('delete', ?, ?)`, rid, oldContent)
	return err
}

// UpdateContent rewrites content (review edit, consolidation date fixes).
// The old content is preserved in the ops payload — reversible.
func (s *Store) UpdateContent(id, newContent, reason string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var old string
	if err := tx.QueryRow(`SELECT content FROM memories WHERE id=?`, id).Scan(&old); err != nil {
		return wrapNotFound(err, id)
	}
	if err := ftsDeleteTx(tx, id, old); err != nil {
		return err
	}
	now := fmtTime(time.Now().UTC())
	if _, err := tx.Exec(`UPDATE memories SET content=?, content_hash=?, updated_at=?, embedding=NULL WHERE id=?`,
		newContent, HashContent(newContent), now, id); err != nil {
		return err
	}
	if err := ftsInsertTx(tx, id, newContent); err != nil {
		return err
	}
	if err := appendOpTx(tx, "edit", id, map[string]any{"old": old, "new": newContent, "reason": reason}); err != nil {
		return err
	}
	return tx.Commit()
}

// SetEmbedding stores a vector for one memory.
func (s *Store) SetEmbedding(id string, v []float32) error {
	_, err := s.DB.Exec(`UPDATE memories SET embedding=? WHERE id=?`, EncodeVector(v), id)
	return err
}

// SetStatus transitions status with a journal entry recording the prior
// state (payload.prev) so it can be reversed.
func (s *Store) SetStatus(id, status, op string, extra map[string]any) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := setStatusTx(tx, id, status, op, extra); err != nil {
		return err
	}
	return tx.Commit()
}

func setStatusTx(tx *sql.Tx, id, status, op string, extra map[string]any) error {
	var prev string
	if err := tx.QueryRow(`SELECT status FROM memories WHERE id=?`, id).Scan(&prev); err != nil {
		return wrapNotFound(err, id)
	}
	now := fmtTime(time.Now().UTC())
	if _, err := tx.Exec(`UPDATE memories SET status=?, updated_at=? WHERE id=?`, status, now, id); err != nil {
		return err
	}
	payload := map[string]any{"prev": prev, "next": status}
	for k, v := range extra {
		payload[k] = v
	}
	return appendOpTx(tx, op, id, payload)
}

// Supersede marks old superseded by new (soft, chained, reversible).
func (s *Store) Supersede(oldID, newID string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var prevStatus, prevBy string
	if err := tx.QueryRow(`SELECT status, superseded_by FROM memories WHERE id=?`, oldID).Scan(&prevStatus, &prevBy); err != nil {
		return wrapNotFound(err, oldID)
	}
	now := fmtTime(time.Now().UTC())
	if _, err := tx.Exec(`UPDATE memories SET status=?, superseded_by=?, updated_at=? WHERE id=?`,
		StatusSuperseded, newID, now, oldID); err != nil {
		return err
	}
	if err := appendOpTx(tx, "supersede", oldID, map[string]any{
		"prev": prevStatus, "prev_superseded_by": prevBy, "superseded_by": newID,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// Reconfirm is passive reconfirmation of an existing belief (§8): bump
// updated_at/last_confirmed_at, raise confidence, optionally importance,
// and promote aging → active.
func (s *Store) Reconfirm(id string, importance int) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var conf float64
	var imp int
	var status string
	if err := tx.QueryRow(`SELECT confidence, importance, status FROM memories WHERE id=?`, id).Scan(&conf, &imp, &status); err != nil {
		return wrapNotFound(err, id)
	}
	newConf := conf + 0.1
	if newConf > 1.0 {
		newConf = 1.0
	}
	newImp := imp
	if importance > imp {
		newImp = importance
	}
	newStatus := status
	if status == StatusAging {
		newStatus = StatusActive
	}
	now := fmtTime(time.Now().UTC())
	if _, err := tx.Exec(`UPDATE memories SET confidence=?, importance=?, status=?, updated_at=?, last_confirmed_at=? WHERE id=?`,
		newConf, newImp, newStatus, now, now, id); err != nil {
		return err
	}
	if err := appendOpTx(tx, "reconfirm", id, map[string]any{
		"confidence": newConf, "prev_confidence": conf, "prev_status": status,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// SetTrust changes the trust tier (review approval), journaled.
func (s *Store) SetTrust(id string, t int, op string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var prev int
	if err := tx.QueryRow(`SELECT trust FROM memories WHERE id=?`, id).Scan(&prev); err != nil {
		return wrapNotFound(err, id)
	}
	now := fmtTime(time.Now().UTC())
	if _, err := tx.Exec(`UPDATE memories SET trust=?, updated_at=? WHERE id=?`, t, now, id); err != nil {
		return err
	}
	return commitOp(tx, op, id, map[string]any{"prev_trust": prev, "trust": t})
}

// SetConfidence updates belief confidence, journaled.
func (s *Store) SetConfidence(id string, c float64, op string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var prev float64
	if err := tx.QueryRow(`SELECT confidence FROM memories WHERE id=?`, id).Scan(&prev); err != nil {
		return wrapNotFound(err, id)
	}
	if _, err := tx.Exec(`UPDATE memories SET confidence=? WHERE id=?`, c, id); err != nil {
		return err
	}
	return commitOp(tx, op, id, map[string]any{"prev_confidence": prev, "confidence": c})
}

func commitOp(tx *sql.Tx, op, id string, payload map[string]any) error {
	if err := appendOpTx(tx, op, id, payload); err != nil {
		return err
	}
	return tx.Commit()
}

// Get returns one memory by exact id or unique prefix (≥6 chars), with
// entities and tags joined.
func (s *Store) Get(idOrPrefix string) (*Memory, error) {
	id := strings.ToLower(strings.TrimSpace(idOrPrefix))
	row := s.DB.QueryRow(`SELECT `+memCols+` FROM memories WHERE id=?`, id)
	m, err := scanMemory(row)
	if errors.Is(err, sql.ErrNoRows) && len(id) >= 6 {
		rows, qerr := s.DB.Query(`SELECT `+memCols+` FROM memories WHERE id LIKE ? LIMIT 3`, id+"%")
		if qerr != nil {
			return nil, qerr
		}
		defer rows.Close()
		var matches []*Memory
		for rows.Next() {
			mm, serr := scanMemory(rows)
			if serr != nil {
				return nil, serr
			}
			matches = append(matches, mm)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		switch len(matches) {
		case 0:
			return nil, fmt.Errorf("%w: %s", ErrNotFound, idOrPrefix)
		case 1:
			m, err = matches[0], nil
		default:
			return nil, fmt.Errorf("%w: %s", ErrAmbiguousID, idOrPrefix)
		}
	} else if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, idOrPrefix)
		}
		return nil, err
	}
	if err := s.attach(m); err != nil {
		return nil, err
	}
	return m, nil
}

func (s *Store) attach(m *Memory) error {
	ents, err := s.EntitiesFor(m.ID)
	if err != nil {
		return err
	}
	m.Entities = ents
	tags, err := s.TagsFor(m.ID)
	if err != nil {
		return err
	}
	m.Tags = tags
	return nil
}

// ListFilter selects memories.
type ListFilter struct {
	Statuses []string
	Types    []string
	Entity   string // entity name or alias (case-insensitive)
	Since    time.Time
	Trusts   []int
	Query    string // optional LIKE filter for browse
	Limit    int
}

// List returns memories matching the filter, newest first.
func (s *Store) List(f ListFilter) ([]*Memory, error) {
	var where []string
	var args []any
	if len(f.Statuses) > 0 {
		where = append(where, `status IN (`+placeholders(len(f.Statuses))+`)`)
		for _, st := range f.Statuses {
			args = append(args, st)
		}
	}
	if len(f.Types) > 0 {
		where = append(where, `type IN (`+placeholders(len(f.Types))+`)`)
		for _, t := range f.Types {
			args = append(args, t)
		}
	}
	if len(f.Trusts) > 0 {
		where = append(where, `trust IN (`+placeholders(len(f.Trusts))+`)`)
		for _, t := range f.Trusts {
			args = append(args, t)
		}
	}
	if !f.Since.IsZero() {
		where = append(where, `updated_at >= ?`)
		args = append(args, fmtTime(f.Since))
	}
	if f.Entity != "" {
		eid, err := s.FindEntity(f.Entity)
		if err != nil {
			return nil, err
		}
		if eid == "" {
			return nil, nil
		}
		where = append(where, `id IN (SELECT memory_id FROM memory_entities WHERE entity_id=?)`)
		args = append(args, eid)
	}
	if f.Query != "" {
		where = append(where, `content LIKE ?`)
		args = append(args, "%"+f.Query+"%")
	}
	q := `SELECT ` + memCols + ` FROM memories`
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, " AND ")
	}
	q += ` ORDER BY updated_at DESC`
	if f.Limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, f.Limit)
	}
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Memory
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// FindByHash returns the newest memory (any status) with this content hash.
func (s *Store) FindByHash(hash string) (*Memory, error) {
	row := s.DB.QueryRow(`SELECT `+memCols+` FROM memories WHERE content_hash=? ORDER BY created_at DESC LIMIT 1`, hash)
	m, err := scanMemory(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return m, err
}

// Chain walks the supersedence chain in both directions from id.
func (s *Store) Chain(id string) (older []*Memory, newer []*Memory, err error) {
	// newer: follow superseded_by pointers forward
	cur := id
	for range 32 {
		var next string
		err := s.DB.QueryRow(`SELECT superseded_by FROM memories WHERE id=?`, cur).Scan(&next)
		if err != nil || next == "" {
			break
		}
		m, gerr := s.Get(next)
		if gerr != nil {
			break
		}
		newer = append(newer, m)
		cur = next
	}
	// older: rows that point at any id in the backward chain
	cur = id
	for range 32 {
		row := s.DB.QueryRow(`SELECT `+memCols+` FROM memories WHERE superseded_by=? ORDER BY created_at DESC LIMIT 1`, cur)
		m, serr := scanMemory(row)
		if serr != nil {
			break
		}
		older = append(older, m)
		cur = m.ID
	}
	return older, newer, nil
}

// CountByStatus returns counts grouped by status.
func (s *Store) CountByStatus() (map[string]int, error) {
	rows, err := s.DB.Query(`SELECT status, COUNT(*) FROM memories GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, rows.Err()
}

// CountBy returns counts grouped by an arbitrary column (type, trust).
func (s *Store) CountBy(col string) (map[string]int, error) {
	switch col {
	case "type", "trust", "scope":
	default:
		return nil, fmt.Errorf("unsupported group column %q", col)
	}
	rows, err := s.DB.Query(`SELECT ` + col + `, COUNT(*) FROM memories GROUP BY ` + col)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, rows.Err()
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func wrapNotFound(err error, id string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return err
}

func tier(i int) trust.Tier { return trust.Tier(i) }

// sortMemoriesByUpdated sorts newest first (used by merged-scope listing).
func SortMemoriesByUpdated(ms []*Memory) {
	sort.Slice(ms, func(i, j int) bool { return ms[i].UpdatedAt.After(ms[j].UpdatedAt) })
}

// MarshalPayload is a helper for op payloads.
func MarshalPayload(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}
