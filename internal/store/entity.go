package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// FindEntity resolves a name or alias to an entity id (case-insensitive).
// Returns "" if unknown.
func (s *Store) FindEntity(name string) (string, error) {
	name = strings.TrimSpace(name)
	var id string
	err := s.DB.QueryRow(`SELECT id FROM entities WHERE name = ? COLLATE NOCASE`, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	// alias scan (entities are few; a table scan is fine)
	rows, err := s.DB.Query(`SELECT id, aliases_json FROM entities`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	lower := strings.ToLower(name)
	for rows.Next() {
		var eid, aj string
		if err := rows.Scan(&eid, &aj); err != nil {
			return "", err
		}
		var aliases []string
		_ = json.Unmarshal([]byte(aj), &aliases)
		for _, a := range aliases {
			if strings.ToLower(a) == lower {
				return eid, nil
			}
		}
	}
	return "", rows.Err()
}

// EnsureEntity finds or creates an entity by name, returning it.
func (s *Store) EnsureEntity(name, etype string) (Entity, error) {
	name = strings.TrimSpace(name)
	if etype == "" {
		etype = "other"
	}
	id, err := s.FindEntity(name)
	if err != nil {
		return Entity{}, err
	}
	if id != "" {
		var e Entity
		var aj string
		if err := s.DB.QueryRow(`SELECT id, name, type, aliases_json FROM entities WHERE id=?`, id).
			Scan(&e.ID, &e.Name, &e.Type, &aj); err != nil {
			return Entity{}, err
		}
		_ = json.Unmarshal([]byte(aj), &e.Aliases)
		// Upgrade type if we previously guessed "other".
		if e.Type == "other" && etype != "other" {
			if _, err := s.DB.Exec(`UPDATE entities SET type=? WHERE id=?`, etype, e.ID); err == nil {
				e.Type = etype
			}
		}
		return e, nil
	}
	e := Entity{ID: NewID(), Name: name, Type: etype}
	if _, err := s.DB.Exec(`INSERT INTO entities(id, name, type, aliases_json) VALUES(?,?,?,?)`,
		e.ID, e.Name, e.Type, "[]"); err != nil {
		return Entity{}, err
	}
	return e, nil
}

// AddAlias records an alternative name for an entity.
func (s *Store) AddAlias(entityID, alias string) error {
	var aj string
	if err := s.DB.QueryRow(`SELECT aliases_json FROM entities WHERE id=?`, entityID).Scan(&aj); err != nil {
		return err
	}
	var aliases []string
	_ = json.Unmarshal([]byte(aj), &aliases)
	for _, a := range aliases {
		if strings.EqualFold(a, alias) {
			return nil
		}
	}
	aliases = append(aliases, alias)
	b, _ := json.Marshal(aliases)
	_, err := s.DB.Exec(`UPDATE entities SET aliases_json=? WHERE id=?`, string(b), entityID)
	return err
}

func linkEntitiesTx(tx *sql.Tx, memoryID string, ents []Entity) error {
	for _, e := range ents {
		if e.ID == "" {
			continue
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO memory_entities(memory_id, entity_id) VALUES(?,?)`,
			memoryID, e.ID); err != nil {
			return err
		}
	}
	return nil
}

func linkTagsTx(tx *sql.Tx, memoryID string, tags []string) error {
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO tags(name) VALUES(?)`, t); err != nil {
			return err
		}
		var tid int64
		if err := tx.QueryRow(`SELECT id FROM tags WHERE name=?`, t).Scan(&tid); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO memory_tags(memory_id, tag_id) VALUES(?,?)`,
			memoryID, tid); err != nil {
			return err
		}
	}
	return nil
}

// LinkEntity attaches an entity to an existing memory.
func (s *Store) LinkEntity(memoryID string, e Entity) error {
	_, err := s.DB.Exec(`INSERT OR IGNORE INTO memory_entities(memory_id, entity_id) VALUES(?,?)`, memoryID, e.ID)
	return err
}

// EntitiesFor returns the entities linked to a memory.
func (s *Store) EntitiesFor(memoryID string) ([]Entity, error) {
	rows, err := s.DB.Query(`SELECT e.id, e.name, e.type, e.aliases_json
		FROM entities e JOIN memory_entities me ON me.entity_id = e.id
		WHERE me.memory_id=? ORDER BY e.name`, memoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entity
	for rows.Next() {
		var e Entity
		var aj string
		if err := rows.Scan(&e.ID, &e.Name, &e.Type, &aj); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(aj), &e.Aliases)
		out = append(out, e)
	}
	return out, rows.Err()
}

// TagsFor returns tag names for a memory.
func (s *Store) TagsFor(memoryID string) ([]string, error) {
	rows, err := s.DB.Query(`SELECT t.name FROM tags t JOIN memory_tags mt ON mt.tag_id=t.id
		WHERE mt.memory_id=? ORDER BY t.name`, memoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ListEntities returns entities with memory counts, optionally filtered by
// type, ordered by count descending.
func (s *Store) ListEntities(etype string) ([]Entity, error) {
	q := `SELECT e.id, e.name, e.type, e.aliases_json,
		(SELECT COUNT(*) FROM memory_entities me JOIN memories m ON m.id = me.memory_id
		 WHERE me.entity_id = e.id AND m.status IN ('active','aging')) AS n
		FROM entities e`
	var args []any
	if etype != "" {
		q += ` WHERE e.type = ?`
		args = append(args, etype)
	}
	q += ` ORDER BY n DESC, e.name`
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entity
	for rows.Next() {
		var e Entity
		var aj string
		if err := rows.Scan(&e.ID, &e.Name, &e.Type, &aj, &e.Count); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(aj), &e.Aliases)
		out = append(out, e)
	}
	return out, rows.Err()
}

// AttachEntities loads entity links for a batch of memories in one query.
func (s *Store) AttachEntities(ms []*Memory) error {
	if len(ms) == 0 {
		return nil
	}
	byID := make(map[string]*Memory, len(ms))
	for _, m := range ms {
		byID[m.ID] = m
	}
	rows, err := s.DB.Query(`SELECT me.memory_id, e.id, e.name, e.type, e.aliases_json
		FROM memory_entities me JOIN entities e ON e.id = me.entity_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var mid string
		var e Entity
		var aj string
		if err := rows.Scan(&mid, &e.ID, &e.Name, &e.Type, &aj); err != nil {
			return err
		}
		if m, ok := byID[mid]; ok {
			_ = json.Unmarshal([]byte(aj), &e.Aliases)
			m.Entities = append(m.Entities, e)
		}
	}
	return rows.Err()
}

// MemoryIDsForEntity returns ids of memories linked to an entity, filtered
// by status set (nil = all).
func (s *Store) MemoryIDsForEntity(entityID string, statuses []string) ([]string, error) {
	q := `SELECT m.id FROM memories m JOIN memory_entities me ON me.memory_id = m.id WHERE me.entity_id = ?`
	args := []any{entityID}
	if len(statuses) > 0 {
		q += ` AND m.status IN (` + placeholders(len(statuses)) + `)`
		for _, st := range statuses {
			args = append(args, st)
		}
	}
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
