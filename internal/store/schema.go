package store

// Schema follows §6 of the build spec. All mutations are soft: status
// transitions and the ops journal make every change reversible.

const schemaV1 = `
CREATE TABLE IF NOT EXISTS memories (
  id                TEXT PRIMARY KEY,
  content           TEXT NOT NULL,
  type              TEXT NOT NULL DEFAULT 'note',      -- fact|preference|decision|event|note
  importance        INTEGER NOT NULL DEFAULT 3,        -- 1..5
  trust             INTEGER NOT NULL DEFAULT 0,        -- 0=user-stated 1=user-approved 2=auto-digest 3=untrusted-origin
  confidence        REAL NOT NULL DEFAULT 1.0,
  last_confirmed_at TEXT NOT NULL,
  scope             TEXT NOT NULL DEFAULT 'global',    -- global|project
  source            TEXT NOT NULL DEFAULT '',
  session_id        TEXT NOT NULL DEFAULT '',
  created_at        TEXT NOT NULL,
  updated_at        TEXT NOT NULL,
  status            TEXT NOT NULL DEFAULT 'active',    -- active|superseded|tombstoned|quarantined|aging
  superseded_by     TEXT NOT NULL DEFAULT '',
  embedding         BLOB,
  content_hash      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_memories_status ON memories(status);
CREATE INDEX IF NOT EXISTS idx_memories_hash   ON memories(content_hash);
CREATE INDEX IF NOT EXISTS idx_memories_type   ON memories(type);

CREATE TABLE IF NOT EXISTS entities (
  id           TEXT PRIMARY KEY,
  name         TEXT NOT NULL,
  type         TEXT NOT NULL DEFAULT 'other',          -- person|project|org|other
  aliases_json TEXT NOT NULL DEFAULT '[]'
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_entities_name ON entities(name COLLATE NOCASE);

CREATE TABLE IF NOT EXISTS memory_entities (
  memory_id TEXT NOT NULL,
  entity_id TEXT NOT NULL,
  PRIMARY KEY (memory_id, entity_id)
);
CREATE INDEX IF NOT EXISTS idx_me_entity ON memory_entities(entity_id);

CREATE TABLE IF NOT EXISTS tags (
  id   INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS memory_tags (
  memory_id TEXT NOT NULL,
  tag_id    INTEGER NOT NULL,
  PRIMARY KEY (memory_id, tag_id)
);

CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
  content,
  content='',
  tokenize='unicode61 remove_diacritics 2'
);

-- Append-only operations journal (decision D14). Never updated, never
-- deleted. Payload is JSON carrying enough state to reverse the op.
CREATE TABLE IF NOT EXISTS ops (
  id        INTEGER PRIMARY KEY,
  ts        TEXT NOT NULL,
  op        TEXT NOT NULL,
  memory_id TEXT NOT NULL DEFAULT '',
  payload   TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_ops_memory ON ops(memory_id);

-- Review flags: items surfaced in the approval inbox beyond quarantine
-- (needs-review for auto-digested T2, ambiguity for unresolved
-- contradictions). Additive to the §6 schema; documented in docs/schema.md.
CREATE TABLE IF NOT EXISTS flags (
  id          INTEGER PRIMARY KEY,
  memory_id   TEXT NOT NULL,
  kind        TEXT NOT NULL,              -- needs-review|ambiguity|instruction-shape
  detail      TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL,
  resolved_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_flags_open ON flags(memory_id) WHERE resolved_at = '';

CREATE TABLE IF NOT EXISTS meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
`

// Meta keys.
const (
	MetaSchemaVersion  = "schema_version"
	MetaEmbeddingModel = "embedding_model" // model identity that produced stored vectors
	MetaEmbeddingDims  = "embedding_dims"
	MetaStoreCreated   = "store_created_at"
	MetaPostureNudged  = "posture_auto_suggested" // F3 one-time suggestion shown
)
