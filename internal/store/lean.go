package store

// Lean access paths for the recall hot loop (§7 p50 < 50ms at 50k):
// never materialize full rows for candidate generation — ids, vectors,
// and ranking metadata only. Full rows are fetched for final hits.

// LeanMeta carries the ranking-relevant columns without content or
// embedding blobs.
type LeanMeta struct {
	ID              string
	Type            string
	Importance      int
	Trust           int
	Confidence      float64
	Status          string
	UpdatedAt       string
	LastConfirmedAt string
}

// VectorEpoch identifies the current vector-relevant state of the store:
// the max ops id over operations that can change embeddings or status.
// The ops journal is append-only, so equality of epochs proves the
// vector cache is current. Injection/export/counter ops are excluded —
// they cannot invalidate vectors.
func (s *Store) VectorEpoch() (int64, error) {
	var epoch int64
	err := s.DB.QueryRow(`SELECT COALESCE(MAX(id), 0) FROM ops WHERE op NOT IN ('inject','export','counter')`).Scan(&epoch)
	return epoch, err
}

// ScanVectors streams (id, embedding) for the given statuses.
func (s *Store) ScanVectors(statuses []string, fn func(id string, vec []float32) error) error {
	q := `SELECT id, embedding FROM memories WHERE embedding IS NOT NULL`
	var args []any
	if len(statuses) > 0 {
		q += ` AND status IN (` + placeholders(len(statuses)) + `)`
		for _, st := range statuses {
			args = append(args, st)
		}
	}
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			return err
		}
		if err := fn(id, DecodeVector(blob)); err != nil {
			return err
		}
	}
	return rows.Err()
}

// FilteredIDs returns the id set matching the pre-fusion filters
// (statuses, types, entity, since) without loading rows.
func (s *Store) FilteredIDs(f ListFilter) (map[string]bool, error) {
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
			return map[string]bool{}, nil
		}
		where = append(where, `id IN (SELECT memory_id FROM memory_entities WHERE entity_id=?)`)
		args = append(args, eid)
	}
	q := `SELECT id FROM memories`
	if len(where) > 0 {
		q += ` WHERE ` + joinAnd(where)
	}
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// GetMany fetches full rows (with entities) for a set of ids in one
// round trip per table.
func (s *Store) GetMany(ids []string) (map[string]*Memory, error) {
	if len(ids) == 0 {
		return map[string]*Memory{}, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.DB.Query(`SELECT `+memCols+` FROM memories WHERE id IN (`+placeholders(len(ids))+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*Memory{}
	var list []*Memory
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		out[m.ID] = m
		list = append(list, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.AttachEntities(list); err != nil {
		return nil, err
	}
	return out, nil
}

func joinAnd(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " AND "
		}
		out += p
	}
	return out
}
