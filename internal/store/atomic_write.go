package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// WriteDecisionKind is the store-level result of belief adjudication.
type WriteDecisionKind string

const (
	WriteNew        WriteDecisionKind = "new"
	WriteDuplicate  WriteDecisionKind = "duplicate"
	WriteSupersedes WriteDecisionKind = "supersedes"
	WriteAmbiguous  WriteDecisionKind = "ambiguous"
)

// WriteDecision tells AtomicWrite how to apply a decision made against the
// candidate snapshot loaded inside its transaction.
type WriteDecision struct {
	Kind       WriteDecisionKind
	ExistingID string
	FlagDetail string
}

// AtomicWriteRequest contains a fully prepared memory and its write policy.
type AtomicWriteRequest struct {
	Memory             *Memory
	Entities           []Entity
	Tags               []string
	EmbeddingModel     string
	EmbeddingDims      int
	QuarantineReason   string
	QuarantineFlagKind string
	Decide             func([]*Memory) WriteDecision
}

// AtomicWriteResult reports the mutation committed by AtomicWrite.
type AtomicWriteResult struct {
	Action    string
	Memory    *Memory
	Existing  *Memory
	Ambiguous *Memory
}

// AtomicWrite serializes adjudication and mutation in one immediate
// transaction. Concurrent writers cannot both decide against a stale snapshot.
func (s *Store) AtomicWrite(req AtomicWriteRequest) (*AtomicWriteResult, error) {
	m := req.Memory
	if m == nil {
		return nil, fmt.Errorf("atomic write: nil memory")
	}
	prepareMemory(m)
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	decision := WriteDecision{Kind: WriteNew}
	var existing *Memory
	if req.Decide != nil {
		candidates, err := adjudicationPoolTx(tx)
		if err != nil {
			return nil, err
		}
		decision = req.Decide(candidates)
		if decision.ExistingID != "" {
			for _, candidate := range candidates {
				if candidate.ID == decision.ExistingID {
					existing = candidate
					break
				}
			}
			if existing == nil {
				return nil, fmt.Errorf("atomic write decision references missing candidate %s", decision.ExistingID)
			}
		}
	}

	if decision.Kind == WriteDuplicate {
		if existing == nil {
			return nil, fmt.Errorf("atomic write duplicate decision requires an existing memory")
		}
		if err := reconfirmTx(tx, existing.ID, m.Importance); err != nil {
			return nil, err
		}
		updated, err := getMemoryTx(tx, existing.ID)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &AtomicWriteResult{Action: "reconfirmed", Memory: updated, Existing: existing}, nil
	}

	if len(m.Embedding) > 0 && req.EmbeddingModel != "" {
		var pinned string
		err := tx.QueryRow(`SELECT value FROM meta WHERE key=?`, MetaEmbeddingModel).Scan(&pinned)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		if pinned != "" && pinned != req.EmbeddingModel {
			return nil, fmt.Errorf("embedding model mismatch: store uses %s, write uses %s", pinned, req.EmbeddingModel)
		}
		for key, value := range map[string]string{
			MetaEmbeddingModel: req.EmbeddingModel,
			MetaEmbeddingDims:  fmt.Sprint(req.EmbeddingDims),
		} {
			if _, err := tx.Exec(`INSERT INTO meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value); err != nil {
				return nil, err
			}
		}
	}
	resolvedEntities := make([]Entity, 0, len(req.Entities))
	for _, entity := range req.Entities {
		resolved, err := ensureImportedEntityTx(tx, entity)
		if err != nil {
			return nil, err
		}
		resolvedEntities = append(resolvedEntities, resolved)
	}
	if err := insertMemoryTx(tx, m, resolvedEntities, req.Tags); err != nil {
		return nil, err
	}
	switch decision.Kind {
	case WriteNew:
	case WriteSupersedes:
		if existing == nil {
			return nil, fmt.Errorf("atomic write supersedes decision requires an existing memory")
		}
		if err := supersedeTx(tx, existing.ID, m.ID); err != nil {
			return nil, err
		}
	case WriteAmbiguous:
		if existing == nil {
			return nil, fmt.Errorf("atomic write ambiguous decision requires an existing memory")
		}
		if _, err := tx.Exec(`INSERT INTO flags(memory_id,kind,detail,created_at) VALUES(?,?,?,?)`,
			m.ID, FlagAmbiguity, decision.FlagDetail, fmtTime(time.Now().UTC())); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("atomic write: unknown decision %q", decision.Kind)
	}
	if m.Status == StatusQuarantined {
		kind := req.QuarantineFlagKind
		if kind == "" {
			kind = FlagTainted
		}
		if _, err := tx.Exec(`INSERT INTO flags(memory_id,kind,detail,created_at) VALUES(?,?,?,?)`,
			m.ID, kind, req.QuarantineReason, fmtTime(time.Now().UTC())); err != nil {
			return nil, err
		}
		if err := appendOpTx(tx, OpQuarantine, m.ID, map[string]any{"reason": req.QuarantineReason}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	m.Entities = resolvedEntities
	m.Tags = req.Tags
	result := &AtomicWriteResult{Action: "created", Memory: m}
	if m.Status == StatusQuarantined {
		result.Action = "quarantined"
	} else if decision.Kind == WriteSupersedes {
		result.Action = "superseded"
	}
	result.Existing = existing
	if decision.Kind == WriteAmbiguous {
		result.Ambiguous = existing
	}
	return result, nil
}

func prepareMemory(m *Memory) {
	now := time.Now().UTC()
	if m.ID == "" {
		m.ID = NewID()
	}
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
}

func insertMemoryTx(tx *sql.Tx, m *Memory, entities []Entity, tags []string) error {
	_, err := tx.Exec(`INSERT INTO memories(`+memCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
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
	return appendOpTx(tx, OpCreate, m.ID, map[string]any{"content": m.Content, "type": m.Type, "trust": int(m.Trust), "status": m.Status, "source": m.Source})
}

func adjudicationPoolTx(tx *sql.Tx) ([]*Memory, error) {
	rows, err := tx.Query(`SELECT `+memCols+` FROM memories WHERE status IN (?,?) ORDER BY updated_at DESC LIMIT 5000`, StatusActive, StatusAging)
	if err != nil {
		return nil, err
	}
	var out []*Memory
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for _, m := range out {
		entities, err := entitiesForTx(tx, m.ID)
		if err != nil {
			return nil, err
		}
		m.Entities = entities
	}
	return out, nil
}

func entitiesForTx(tx *sql.Tx, memoryID string) ([]Entity, error) {
	rows, err := tx.Query(`SELECT e.id,e.name,e.type,e.aliases_json FROM entities e JOIN memory_entities me ON me.entity_id=e.id WHERE me.memory_id=? ORDER BY e.name`, memoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entity
	for rows.Next() {
		var e Entity
		var aliases string
		if err := rows.Scan(&e.ID, &e.Name, &e.Type, &aliases); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(aliases), &e.Aliases)
		out = append(out, e)
	}
	return out, rows.Err()
}

func getMemoryTx(tx *sql.Tx, id string) (*Memory, error) {
	m, err := scanMemory(tx.QueryRow(`SELECT `+memCols+` FROM memories WHERE id=?`, id))
	if err != nil {
		return nil, err
	}
	m.Entities, err = entitiesForTx(tx, id)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(`SELECT t.name FROM tags t JOIN memory_tags mt ON mt.tag_id=t.id WHERE mt.memory_id=? ORDER BY t.name`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			rows.Close()
			return nil, err
		}
		m.Tags = append(m.Tags, tag)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return m, nil
}

func reconfirmTx(tx *sql.Tx, id string, importance int) error {
	var confidence float64
	var currentImportance int
	var status string
	if err := tx.QueryRow(`SELECT confidence,importance,status FROM memories WHERE id=?`, id).Scan(&confidence, &currentImportance, &status); err != nil {
		return err
	}
	newConfidence := confidence + 0.1
	if newConfidence > 1 {
		newConfidence = 1
	}
	if importance < currentImportance {
		importance = currentImportance
	}
	if importance == 0 {
		importance = currentImportance
	}
	newStatus := status
	if status == StatusAging {
		newStatus = StatusActive
	}
	now := fmtTime(time.Now().UTC())
	if _, err := tx.Exec(`UPDATE memories SET confidence=?,importance=?,status=?,updated_at=?,last_confirmed_at=? WHERE id=?`, newConfidence, importance, newStatus, now, now, id); err != nil {
		return err
	}
	return appendOpTx(tx, OpReconfirm, id, map[string]any{"confidence": newConfidence, "prev_confidence": confidence, "prev_status": status})
}

func supersedeTx(tx *sql.Tx, oldID, newID string) error {
	var prevStatus, prevBy string
	if err := tx.QueryRow(`SELECT status,superseded_by FROM memories WHERE id=?`, oldID).Scan(&prevStatus, &prevBy); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE memories SET status=?,superseded_by=?,updated_at=? WHERE id=?`, StatusSuperseded, newID, fmtTime(time.Now().UTC()), oldID); err != nil {
		return err
	}
	return appendOpTx(tx, OpSupersede, oldID, map[string]any{"prev": prevStatus, "prev_superseded_by": prevBy, "superseded_by": newID})
}
