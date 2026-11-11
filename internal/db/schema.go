package db

// FormatVersion is the index format stored in PRAGMA user_version. Format 2
// changed symbol identity; databases created earlier report 0.
const FormatVersion = 2

// SQL statements for creating the database schema
const (
	CreateSymbolsTable = `
CREATE TABLE IF NOT EXISTS symbols (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    kind TEXT NOT NULL,
    file TEXT NOT NULL,
    line INTEGER NOT NULL,
    column INTEGER NOT NULL,
    start_line INTEGER NOT NULL DEFAULT 0,
    start_column INTEGER NOT NULL DEFAULT 0,
    end_line INTEGER,
    end_column INTEGER,
    scope TEXT,
    signature TEXT,
    documentation TEXT,
    language TEXT NOT NULL,
    source TEXT DEFAULT 'lsp',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);`

	CreateCallsTable = `
CREATE TABLE IF NOT EXISTS calls (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    caller_id TEXT NOT NULL,
    callee_id TEXT NOT NULL,
    file TEXT NOT NULL,
    line INTEGER NOT NULL,
    column INTEGER NOT NULL,
    UNIQUE(caller_id, callee_id, file, line, column),
    FOREIGN KEY(caller_id) REFERENCES symbols(id),
    FOREIGN KEY(callee_id) REFERENCES symbols(id)
);`

	CreateTypeHierarchyTable = `
CREATE TABLE IF NOT EXISTS type_hierarchy (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    child_id TEXT NOT NULL,
    parent_id TEXT NOT NULL,
    relationship TEXT NOT NULL,
    FOREIGN KEY(child_id) REFERENCES symbols(id),
    FOREIGN KEY(parent_id) REFERENCES symbols(id)
);`

	// CreateUnresolvedTable holds observed relationship sites whose target
	// could not be identified safely. They are never resolved edges (I06).
	CreateUnresolvedTable = `
CREATE TABLE IF NOT EXISTS unresolved_relationships (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    kind TEXT NOT NULL,
    language TEXT NOT NULL,
    from_id TEXT NOT NULL DEFAULT '',
    target_name TEXT NOT NULL,
    file TEXT NOT NULL,
    line INTEGER NOT NULL,
    column INTEGER NOT NULL,
    reason TEXT NOT NULL,
    candidates INTEGER NOT NULL DEFAULT 0,
    UNIQUE(kind, file, line, column, from_id, target_name)
);`

	CreateFileMetaTable = `
CREATE TABLE IF NOT EXISTS file_meta (
    path TEXT PRIMARY KEY,
    mod_time TIMESTAMP NOT NULL,
    language TEXT NOT NULL,
    content_hash TEXT NOT NULL DEFAULT '',
    provider TEXT NOT NULL DEFAULT '',
    extracted_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);`

	// CreateProviderStateTable stores, per language, the digest of the
	// provider configuration and capabilities a snapshot was extracted
	// with. A change invalidates that language's extraction (R02).
	CreateProviderStateTable = `
CREATE TABLE IF NOT EXISTS provider_state (
    language TEXT PRIMARY KEY,
    digest TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);`

	// Indexes for faster queries
	CreateIndexes = `
CREATE INDEX IF NOT EXISTS idx_symbols_name ON symbols(name);
CREATE INDEX IF NOT EXISTS idx_symbols_file ON symbols(file);
CREATE INDEX IF NOT EXISTS idx_symbols_kind ON symbols(kind);
CREATE INDEX IF NOT EXISTS idx_symbols_language ON symbols(language);
CREATE INDEX IF NOT EXISTS idx_calls_caller ON calls(caller_id);
CREATE INDEX IF NOT EXISTS idx_calls_callee ON calls(callee_id);
CREATE INDEX IF NOT EXISTS idx_type_hierarchy_child ON type_hierarchy(child_id);
CREATE INDEX IF NOT EXISTS idx_type_hierarchy_parent ON type_hierarchy(parent_id);
`

	// RepairCallsUniqueness backfills the call-site uniqueness that the
	// calls table's UNIQUE constraint only gives databases created after
	// it was added: duplicates collapse to their first row, then the index
	// enforces the constraint from then on. Idempotent on every run.
	RepairCallsUniqueness = `
DELETE FROM calls WHERE id NOT IN (SELECT MIN(id) FROM calls GROUP BY caller_id, callee_id, file, line, column);
CREATE UNIQUE INDEX IF NOT EXISTS idx_calls_site ON calls(caller_id, callee_id, file, line, column);
`
)

// graphTables are the tables a format reset drops, children first.
var graphTables = []string{"calls", "type_hierarchy", "unresolved_relationships", "symbols", "file_meta", "provider_state"}

// AllSchemaStatements returns all SQL statements needed to create the schema
func AllSchemaStatements() []string {
	return []string{
		CreateSymbolsTable,
		CreateCallsTable,
		CreateTypeHierarchyTable,
		CreateUnresolvedTable,
		CreateFileMetaTable,
		CreateProviderStateTable,
		CreateIndexes,
		RepairCallsUniqueness,
	}
}

// fileMetaColumns are the columns a format-2 database can be missing; they
// arrive additively, never by a format bump. Rows that predate a column
// keep its default, and an empty content hash never skips a file.
var fileMetaColumns = []string{
	"content_hash TEXT NOT NULL DEFAULT ''",
	"provider TEXT NOT NULL DEFAULT ''",
	// ALTER TABLE cannot add a column with CURRENT_TIMESTAMP (a non-constant
	// default), so migration stamps the epoch; new rows always write a real
	// extraction time explicitly.
	"extracted_at TIMESTAMP NOT NULL DEFAULT '1970-01-01 00:00:00'",
}
