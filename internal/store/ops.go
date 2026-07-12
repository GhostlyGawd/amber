package store

import (
	"database/sql"
	"encoding/json"
	"strconv"
	"time"
)

// Op names written to the journal. The journal is append-only: rows are
// never updated or deleted (decision D14). It powers undo/restore today
// and makes future sync a merge problem, not a rewrite.
const (
	OpCreate      = "create"
	OpEdit        = "edit"
	OpReconfirm   = "reconfirm"
	OpSupersede   = "supersede"
	OpTombstone   = "tombstone"
	OpRestore     = "restore"
	OpQuarantine  = "quarantine"
	OpApprove     = "approve"
	OpReject      = "reject"
	OpAge         = "age"
	OpUnage       = "unage"
	OpConsolidate = "consolidate"
	OpReembed     = "reembed"
	OpInject      = "inject"
	OpImport      = "import"
	OpExport      = "export"
	OpCounter     = "counter"
)

func appendOpTx(tx *sql.Tx, op, memoryID string, payload map[string]any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		b = []byte(`{}`)
	}
	_, err = tx.Exec(`INSERT INTO ops(ts, op, memory_id, payload) VALUES(?,?,?,?)`,
		fmtTime(time.Now().UTC()), op, memoryID, string(b))
	return err
}

// AppendOp writes a journal entry outside any other transaction.
func (s *Store) AppendOp(op, memoryID string, payload map[string]any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		b = []byte(`{}`)
	}
	_, err = s.DB.Exec(`INSERT INTO ops(ts, op, memory_id, payload) VALUES(?,?,?,?)`,
		fmtTime(time.Now().UTC()), op, memoryID, string(b))
	return err
}

// OpsFor returns journal entries for a memory, oldest first.
func (s *Store) OpsFor(memoryID string) ([]Op, error) {
	rows, err := s.DB.Query(`SELECT id, ts, op, memory_id, payload FROM ops WHERE memory_id=? ORDER BY id`, memoryID)
	if err != nil {
		return nil, err
	}
	return scanOps(rows)
}

// RecentOps returns the latest n journal entries, newest first, optionally
// filtered by op name.
func (s *Store) RecentOps(n int, op string) ([]Op, error) {
	q := `SELECT id, ts, op, memory_id, payload FROM ops`
	var args []any
	if op != "" {
		q += ` WHERE op=?`
		args = append(args, op)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, n)
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	return scanOps(rows)
}

func scanOps(rows *sql.Rows) ([]Op, error) {
	defer rows.Close()
	var out []Op
	for rows.Next() {
		var o Op
		var ts, payload string
		if err := rows.Scan(&o.ID, &ts, &o.Op, &o.MemoryID, &payload); err != nil {
			return nil, err
		}
		o.TS = parseTime(ts)
		o.Payload = json.RawMessage(payload)
		out = append(out, o)
	}
	return out, rows.Err()
}

// BumpCounter increments a local usage counter in meta (counters-only
// stats; never leaves the machine — see docs/privacy.md).
func (s *Store) BumpCounter(name string) {
	_, _ = s.DB.Exec(`INSERT INTO meta(key, value) VALUES('counter.'||?, '1')
		ON CONFLICT(key) DO UPDATE SET value = CAST(CAST(value AS INTEGER)+1 AS TEXT)`, name)
}

// Counters returns all local usage counters.
func (s *Store) Counters() (map[string]int64, error) {
	rows, err := s.DB.Query(`SELECT key, value FROM meta WHERE key LIKE 'counter.%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		n, _ := strconv.ParseInt(v, 10, 64)
		out[k[len("counter."):]] = n
	}
	return out, rows.Err()
}
