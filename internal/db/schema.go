package db

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

	CreateFileMetaTable = `
CREATE TABLE IF NOT EXISTS file_meta (
    path TEXT PRIMARY KEY,
    mod_time TIMESTAMP NOT NULL,
    language TEXT NOT NULL
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
)

// AllSchemaStatements returns all SQL statements needed to create the schema
func AllSchemaStatements() []string {
	return []string{
		CreateSymbolsTable,
		CreateCallsTable,
		CreateTypeHierarchyTable,
		CreateFileMetaTable,
		CreateIndexes,
	}
}
