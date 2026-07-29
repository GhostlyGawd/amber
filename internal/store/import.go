package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ImportItem is one validated interchange record ready for atomic import.
type ImportItem struct {
	Memory     *Memory
	Entities   []Entity
	Tags       []string
	OriginalID string
}

// ImportBatch inserts a validated set of records in one transaction.
// IDs, timestamps, supersedence, entity aliases, and tags are preserved.
func (s *Store) ImportBatch(items []ImportItem) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, item := range items {
		m := item.Memory
		_, err := tx.Exec(`INSERT INTO memories(`+memCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			m.ID, m.Content, m.Type, m.Importance, int(m.Trust), m.Confidence, fmtTime(m.LastConfirmedAt),
			m.Scope, m.Source, m.SessionID, fmtTime(m.CreatedAt), fmtTime(m.UpdatedAt), m.Status,
			m.SupersededBy, nil, m.ContentHash)
		if err != nil {
			return fmt.Errorf("import memory %s: %w", item.OriginalID, err)
		}
		if err := ftsInsertTx(tx, m.ID, m.Content); err != nil {
			return err
		}
		entities := make([]Entity, 0, len(item.Entities))
		for _, entity := range item.Entities {
			resolved, err := ensureImportedEntityTx(tx, entity)
			if err != nil {
				return err
			}
			entities = append(entities, resolved)
		}
		if err := linkEntitiesTx(tx, m.ID, entities); err != nil {
			return err
		}
		if err := linkTagsTx(tx, m.ID, item.Tags); err != nil {
			return err
		}
		if err := appendOpTx(tx, OpImport, m.ID, map[string]any{"orig_id": item.OriginalID}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func ensureImportedEntityTx(tx *sql.Tx, incoming Entity) (Entity, error) {
	incoming.Name = strings.TrimSpace(incoming.Name)
	if incoming.Name == "" {
		return Entity{}, fmt.Errorf("import entity has empty name")
	}
	if incoming.Type == "" {
		incoming.Type = "other"
	}

	var existing Entity
	var aliasesJSON string
	err := tx.QueryRow(`SELECT id, name, type, aliases_json FROM entities WHERE name=? COLLATE NOCASE`, incoming.Name).
		Scan(&existing.ID, &existing.Name, &existing.Type, &aliasesJSON)
	if err != nil && err != sql.ErrNoRows {
		return Entity{}, err
	}
	if err == sql.ErrNoRows {
		rows, scanErr := tx.Query(`SELECT id,name,type,aliases_json FROM entities`)
		if scanErr != nil {
			return Entity{}, scanErr
		}
		for rows.Next() {
			var candidate Entity
			var candidateAliases string
			if scanErr := rows.Scan(&candidate.ID, &candidate.Name, &candidate.Type, &candidateAliases); scanErr != nil {
				rows.Close()
				return Entity{}, scanErr
			}
			_ = json.Unmarshal([]byte(candidateAliases), &candidate.Aliases)
			for _, alias := range candidate.Aliases {
				if strings.EqualFold(strings.TrimSpace(alias), incoming.Name) {
					existing = candidate
					aliasesJSON = candidateAliases
					err = nil
					break
				}
			}
			if err == nil {
				break
			}
		}
		if scanErr := rows.Close(); scanErr != nil {
			if scanErr := rows.Err(); scanErr != nil {
				rows.Close()
				return Entity{}, scanErr
			}
			return Entity{}, scanErr
		}
	}
	if err == sql.ErrNoRows {
		incoming.ID = NewID()
		incoming.Aliases = cleanAliases(incoming.Aliases, incoming.Name)
		encoded, _ := json.Marshal(incoming.Aliases)
		_, err = tx.Exec(`INSERT INTO entities(id,name,type,aliases_json) VALUES(?,?,?,?)`,
			incoming.ID, incoming.Name, incoming.Type, string(encoded))
		return incoming, err
	}

	_ = json.Unmarshal([]byte(aliasesJSON), &existing.Aliases)
	existing.Aliases = cleanAliases(append(existing.Aliases, incoming.Aliases...), existing.Name)
	if existing.Type == "other" && incoming.Type != "other" {
		existing.Type = incoming.Type
	}
	encoded, _ := json.Marshal(existing.Aliases)
	_, err = tx.Exec(`UPDATE entities SET type=?, aliases_json=? WHERE id=?`, existing.Type, string(encoded), existing.ID)
	return existing, err
}

func cleanAliases(aliases []string, canonical string) []string {
	seen := map[string]bool{strings.ToLower(strings.TrimSpace(canonical)): true}
	out := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		alias = strings.TrimSpace(alias)
		key := strings.ToLower(alias)
		if alias == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, alias)
	}
	sort.Strings(out)
	return out
}
