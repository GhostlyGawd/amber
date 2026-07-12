package store

import "time"

// Flag kinds surfaced in the review inbox.
const (
	FlagNeedsReview      = "needs-review"      // auto-digested T2 awaiting curation
	FlagAmbiguity        = "ambiguity"         // possible contradiction kept as both
	FlagInstructionShape = "instruction-shape" // declarative screen tripped
	FlagTainted          = "tainted"           // derived from tool/web output
)

// AddFlag records a review-inbox marker for a memory.
func (s *Store) AddFlag(memoryID, kind, detail string) error {
	_, err := s.DB.Exec(`INSERT INTO flags(memory_id, kind, detail, created_at) VALUES(?,?,?,?)`,
		memoryID, kind, detail, fmtTime(time.Now().UTC()))
	return err
}

// OpenFlags returns unresolved flags, oldest first.
func (s *Store) OpenFlags() ([]Flag, error) {
	rows, err := s.DB.Query(`SELECT id, memory_id, kind, detail, created_at FROM flags WHERE resolved_at='' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Flag
	for rows.Next() {
		var f Flag
		var created string
		if err := rows.Scan(&f.ID, &f.MemoryID, &f.Kind, &f.Detail, &created); err != nil {
			return nil, err
		}
		f.CreatedAt = parseTime(created)
		out = append(out, f)
	}
	return out, rows.Err()
}

// FlagsFor returns unresolved flags for one memory.
func (s *Store) FlagsFor(memoryID string) ([]Flag, error) {
	rows, err := s.DB.Query(`SELECT id, memory_id, kind, detail, created_at FROM flags WHERE memory_id=? AND resolved_at='' ORDER BY id`, memoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Flag
	for rows.Next() {
		var f Flag
		var created string
		if err := rows.Scan(&f.ID, &f.MemoryID, &f.Kind, &f.Detail, &created); err != nil {
			return nil, err
		}
		f.CreatedAt = parseTime(created)
		out = append(out, f)
	}
	return out, rows.Err()
}

// ResolveFlags marks all open flags for a memory as resolved.
func (s *Store) ResolveFlags(memoryID string) error {
	_, err := s.DB.Exec(`UPDATE flags SET resolved_at=? WHERE memory_id=? AND resolved_at=''`,
		fmtTime(time.Now().UTC()), memoryID)
	return err
}
