// Package store owns the SQLite file: schema, migrations, memory and
// entity CRUD, and the append-only ops journal.
//
// Concurrency: WAL mode plus BEGIN IMMEDIATE around check-and-write so
// multiple agents/panes on one machine share a store safely. Worst case a
// race degrades to a flagged ambiguity, never data loss (§6).
package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/ghostlygawd/amber/internal/trust"
	"github.com/ghostlygawd/amber/internal/version"
)

// Memory statuses.
const (
	StatusActive      = "active"
	StatusSuperseded  = "superseded"
	StatusTombstoned  = "tombstoned"
	StatusQuarantined = "quarantined"
	StatusAging       = "aging"
)

// Memory types.
var Types = []string{"fact", "preference", "decision", "event", "note"}

// ValidType reports whether t is a known memory type.
func ValidType(t string) bool {
	for _, k := range Types {
		if t == k {
			return true
		}
	}
	return false
}

// Memory is one belief record.
type Memory struct {
	ID              string     `json:"id"`
	Content         string     `json:"content"`
	Type            string     `json:"type"`
	Importance      int        `json:"importance"`
	Trust           trust.Tier `json:"trust"`
	Confidence      float64    `json:"confidence"`
	LastConfirmedAt time.Time  `json:"last_confirmed_at"`
	Scope           string     `json:"scope"`
	Source          string     `json:"source"`
	SessionID       string     `json:"session_id,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	Status          string     `json:"status"`
	SupersededBy    string     `json:"superseded_by,omitempty"`
	Embedding       []float32  `json:"-"`
	ContentHash     string     `json:"content_hash"`

	// Joined, not columns:
	Entities []Entity `json:"entities,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

// Entity is a linked person/project/org.
type Entity struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Aliases []string `json:"aliases,omitempty"`
	Count   int      `json:"memory_count,omitempty"`
}

// Op is one journal entry.
type Op struct {
	ID       int64           `json:"id"`
	TS       time.Time       `json:"ts"`
	Op       string          `json:"op"`
	MemoryID string          `json:"memory_id,omitempty"`
	Payload  json.RawMessage `json:"payload,omitempty"`
}

// Flag is a review-inbox marker.
type Flag struct {
	ID        int64
	MemoryID  string
	Kind      string
	Detail    string
	CreatedAt time.Time
	Resolved  bool
}

// Store wraps one SQLite database.
type Store struct {
	DB   *sql.DB
	Dir  string // store directory (~/.amber or <proj>/.amber)
	Path string // db file path
}

// ErrNotInitialized is returned by Open when the DB file does not exist.
var ErrNotInitialized = errors.New("store not initialized (run `amber init`)")

// Open opens an existing store. It refuses to create one implicitly.
func Open(dir string) (*Store, error) {
	path := filepath.Join(dir, "amber.db")
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("%w: no store at %s", ErrNotInitialized, path)
	}
	return open(dir, path)
}

// Create initializes a new store directory and database (0700/0600).
func Create(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "amber.db")
	s, err := open(dir, path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil && !os.IsNotExist(err) {
		s.Close()
		return nil, err
	}
	return s, nil
}

func open(dir, path string) (*Store, error) {
	dsn := "file:" + url.PathEscape(path) +
		"?_txlock=immediate" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite is single-writer; keep one connection to avoid lock churn.
	db.SetMaxOpenConns(1)
	s := &Store{DB: db, Dir: dir, Path: path}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) migrate() error {
	if _, err := s.DB.Exec(schemaV1); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	cur, _ := s.GetMeta(MetaSchemaVersion)
	if cur == "" {
		if err := s.SetMeta(MetaSchemaVersion, fmt.Sprint(version.SchemaVersion)); err != nil {
			return err
		}
	}
	if v, _ := s.GetMeta(MetaStoreCreated); v == "" {
		_ = s.SetMeta(MetaStoreCreated, time.Now().UTC().Format(time.RFC3339))
	}
	return nil
}

// --- meta ---

// GetMeta reads a meta key ("" if absent).
func (s *Store) GetMeta(key string) (string, error) {
	var v string
	err := s.DB.QueryRow(`SELECT value FROM meta WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// SetMeta upserts a meta key.
func (s *Store) SetMeta(key, value string) error {
	_, err := s.DB.Exec(`INSERT INTO meta(key,value) VALUES(?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// --- ids & hashing ---

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// NewID returns a 128-bit random identifier in lowercase Crockford
// base32 (26 chars). Deliberately not time-prefixed: short display
// prefixes (8 chars) stay unique across same-millisecond writes, and
// ordering lives in created_at, not the id.
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		binary.BigEndian.PutUint64(b[:8], uint64(time.Now().UnixNano()))
		binary.BigEndian.PutUint64(b[8:], uint64(time.Now().UnixMilli())^0x9e3779b97f4a7c15)
	}
	// 16 bytes = 128 bits -> 26 base32 chars (130 bits, top 2 padded).
	dst := make([]byte, 26)
	var acc uint32
	var bits uint
	j := 25
	for i := 15; i >= 0; i-- {
		acc |= uint32(b[i]) << bits
		bits += 8
		for bits >= 5 && j >= 0 {
			dst[j] = crockford[acc&31]
			acc >>= 5
			bits -= 5
			j--
		}
	}
	for j >= 0 {
		dst[j] = crockford[acc&31]
		acc >>= 5
		j--
	}
	return strings.ToLower(string(dst))
}

var wsRe = regexp.MustCompile(`\s+`)
var punctRe = regexp.MustCompile(`[^\p{L}\p{N} ]+`)

// NormalizeContent lowercases, strips punctuation, and collapses
// whitespace — the basis for near-duplicate hashing.
func NormalizeContent(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = punctRe.ReplaceAllString(s, " ")
	s = wsRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// HashContent returns the sha256 hex of the normalized content.
func HashContent(s string) string {
	sum := sha256.Sum256([]byte(NormalizeContent(s)))
	return hex.EncodeToString(sum[:])
}

// --- time codec ---

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// --- embeddings codec ---

// EncodeVector serializes a []float32 as little-endian bytes.
func EncodeVector(v []float32) []byte {
	if len(v) == 0 {
		return nil
	}
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

// DecodeVector parses a little-endian float32 blob.
func DecodeVector(b []byte) []float32 {
	if len(b) < 4 {
		return nil
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}
