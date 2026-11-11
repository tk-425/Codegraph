package db

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Manager handles database operations
type Manager struct {
	db     *sql.DB
	dbPath string
}

// busyTimeoutMS bounds how long a writer or reader waits on a lock
// before failing explicitly (T22). A var so tests can shorten it.
var busyTimeoutMS = 5000

// NewManager creates a new database manager
func NewManager(dbPath string) (*Manager, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	absPath, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	// Foreign keys are a per-connection setting, so the DSN carries it to
	// every pooled connection. Transactions take the write lock at BEGIN
	// (BEGIN IMMEDIATE), so a second publisher fails within the busy
	// timeout instead of deadlocking on a deferred upgrade; readers get a
	// bounded wait for a commit window and never see a mixture (T22).
	db, err := sql.Open("sqlite3", fileDSN(absPath, fmt.Sprintf("_foreign_keys=on&_busy_timeout=%d&_txlock=immediate", busyTimeoutMS)))
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	return &Manager{db: db, dbPath: dbPath}, nil
}

// fileDSN is a go-sqlite3 URI DSN for an absolute path. The path is
// percent-encoded, so '?', '#' and spaces in directory names stay part of it.
func fileDSN(absPath, query string) string {
	return (&url.URL{Scheme: "file", Path: absPath, RawQuery: query}).String()
}

// OpenReadOnly opens an existing database for queries. It never creates the
// file or its directory, never migrates, and rejects writes. A missing file
// returns an error wrapping os.ErrNotExist.
func OpenReadOnly(dbPath string) (*Manager, error) {
	absPath, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(absPath); err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	db, err := sql.Open("sqlite3", fileDSN(absPath, fmt.Sprintf("mode=ro&_busy_timeout=%d", busyTimeoutMS)))
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	return &Manager{db: db, dbPath: dbPath}, nil
}

// Initialize creates all tables and indexes. A database created here is
// stamped with the current FormatVersion, and a format-2 database created
// before the file_meta fingerprint columns gets them added additively
// (never by a format bump; queries never run this, only the build path).
func (m *Manager) Initialize() error {
	var existing int
	if err := m.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'symbols'`).Scan(&existing); err != nil {
		return fmt.Errorf("failed to inspect schema: %w", err)
	}
	for _, stmt := range AllSchemaStatements() {
		if _, err := m.db.Exec(stmt); err != nil {
			return fmt.Errorf("failed to execute schema statement: %w", err)
		}
	}
	if err := m.addMissingFileMetaColumns(); err != nil {
		return err
	}
	if existing == 0 {
		if _, err := m.db.Exec(fmt.Sprintf("PRAGMA user_version = %d", FormatVersion)); err != nil {
			return fmt.Errorf("failed to stamp format version: %w", err)
		}
	}
	return nil
}

// addMissingFileMetaColumns inspects file_meta and adds any fingerprint
// column an older format-2 database lacks.
func (m *Manager) addMissingFileMetaColumns() error {
	rows, err := m.db.Query(`PRAGMA table_info(file_meta)`)
	if err != nil {
		return fmt.Errorf("failed to inspect file_meta: %w", err)
	}
	defer rows.Close()
	present := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull int
		var dfltValue any
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &dfltValue, &pk); err != nil {
			return err
		}
		present[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, column := range fileMetaColumns {
		name, _, _ := strings.Cut(column, " ")
		if present[name] {
			continue
		}
		if _, err := m.db.Exec("ALTER TABLE file_meta ADD COLUMN " + column); err != nil {
			return fmt.Errorf("failed to extend file_meta with %s: %w", name, err)
		}
	}
	return nil
}

// IncompatibleFormatError reports a database whose graph was written in
// another index format. Its identities cannot be read with current rules.
type IncompatibleFormatError struct {
	Found int
}

func (e *IncompatibleFormatError) Error() string {
	return fmt.Sprintf("index format %d is incompatible with this version (format %d); run 'codegraph build --force' to rebuild", e.Found, FormatVersion)
}

// CheckFormat returns *IncompatibleFormatError when the database holds a
// graph in another index format. A database without graph tables is
// compatible. It never writes.
func (m *Manager) CheckFormat() error {
	var tables int
	if err := m.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'symbols'`).Scan(&tables); err != nil {
		return fmt.Errorf("failed to inspect schema: %w", err)
	}
	if tables == 0 {
		return nil
	}
	var version int
	if err := m.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("failed to read format version: %w", err)
	}
	if version != FormatVersion {
		return &IncompatibleFormatError{Found: version}
	}
	return nil
}

// ResetFormat's drop-recreate-stamp sequence now runs inside a
// publication transaction (RefreshPlan.ResetFormat) so a failed rebuild
// cannot erase a legacy snapshot. Only Publish resets.

// Close closes the database connection
func (m *Manager) Close() error {
	return m.db.Close()
}

// BeginFile is the graphStore hook for a provider attempt starting at a
// file. The published graph has nothing buffered, so it is a no-op here.
func (m *Manager) BeginFile(path string) {}

// InsertSymbol inserts a symbol into the database
func (m *Manager) InsertSymbol(s *Symbol) error {
	_, err := m.db.Exec(`
		INSERT OR REPLACE INTO symbols (`+symbolColumns("")+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.Name, s.Kind, s.File, s.Line, s.Column, s.StartLine, s.StartColumn,
		s.EndLine, s.EndColumn, s.Scope, s.Signature, s.Documentation, s.Language,
		s.Source, s.CreatedAt,
	)
	return err
}

// InsertCall inserts a call relationship
func (m *Manager) InsertCall(c *Call) error {
	_, err := m.db.Exec(`
		INSERT OR IGNORE INTO calls (caller_id, callee_id, file, line, column)
		VALUES (?, ?, ?, ?, ?)`,
		c.CallerID, c.CalleeID, c.File, c.Line, c.Column,
	)
	return err
}

// InsertTypeHierarchy inserts a type relationship
func (m *Manager) InsertTypeHierarchy(th *TypeHierarchy) error {
	_, err := m.db.Exec(`
		INSERT INTO type_hierarchy (child_id, parent_id, relationship)
		VALUES (?, ?, ?)`,
		th.ChildID, th.ParentID, th.Relationship,
	)
	return err
}

// InsertUnresolved records an unresolved relationship site once.
func (m *Manager) InsertUnresolved(u *Unresolved) error {
	_, err := m.db.Exec(`
		INSERT OR IGNORE INTO unresolved_relationships
		(kind, language, from_id, target_name, file, line, column, reason, candidates)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.Kind, u.Language, u.FromID, u.TargetName, u.File, u.Line, u.Column, u.Reason, u.Candidates,
	)
	return err
}

// GetUnresolved returns every recorded unresolved relationship site.
func (m *Manager) GetUnresolved() ([]Unresolved, error) {
	rows, err := m.db.Query(`
		SELECT kind, language, from_id, target_name, file, line, column, reason, candidates
		FROM unresolved_relationships ORDER BY file, line, column`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var found []Unresolved
	for rows.Next() {
		var u Unresolved
		if err := rows.Scan(&u.Kind, &u.Language, &u.FromID, &u.TargetName, &u.File, &u.Line, &u.Column, &u.Reason, &u.Candidates); err != nil {
			return nil, err
		}
		found = append(found, u)
	}
	return found, rows.Err()
}

// GetImplementations returns symbols that implement/extend the given parent symbol
func (m *Manager) GetImplementations(parentID string) ([]Symbol, error) {
	query := `
		SELECT ` + symbolColumns("s") + `
		FROM symbols s
		INNER JOIN type_hierarchy th ON s.id = th.child_id
		WHERE th.parent_id = ?
		ORDER BY s.file, s.line`

	rows, err := m.db.Query(query, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanSymbols(rows)
}

// GetImplementationsByName returns symbols that implement or extend a type
// by its name, optionally limited to languages. Embedding is not
// implementation, so embeds relationships are excluded.
func (m *Manager) GetImplementationsByName(typeName string, languages []string) ([]Symbol, error) {
	query := `
		SELECT ` + symbolColumns("s") + `
		FROM symbols s
		INNER JOIN type_hierarchy th ON s.id = th.child_id
		INNER JOIN symbols parent ON th.parent_id = parent.id
		WHERE parent.name = ? AND th.relationship IN ('implements', 'extends')`
	args := []interface{}{typeName}

	if len(languages) > 0 {
		query += " AND s.language IN (?" + repeatString(",?", len(languages)-1) + ")"
		for _, lang := range languages {
			args = append(args, lang)
		}
	}
	query += " ORDER BY s.file, s.line"

	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanSymbols(rows)
}

// SearchSymbols searches for symbols by name with optional filters
func (m *Manager) SearchSymbols(name string, kind string, languages []string) ([]Symbol, error) {
	query := "SELECT " + symbolColumns("") + " FROM symbols WHERE name LIKE ?"
	args := []interface{}{"%" + name + "%"}

	if kind != "" {
		query += " AND kind = ?"
		args = append(args, kind)
	} else {
		// By default, exclude module/package declarations from search results
		query += " AND kind != 'module'"
	}

	if len(languages) > 0 {
		query += " AND language IN (?" + repeatString(",?", len(languages)-1) + ")"
		for _, lang := range languages {
			args = append(args, lang)
		}
	}

	query += " ORDER BY name, file, line"

	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanSymbols(rows)
}

// GetCallers finds all callers of a symbol with call site info
func (m *Manager) GetCallers(symbolName string, languages []string) ([]CallerInfo, error) {
	// Match the callee symbol's name, never its ID encoding:
	// - Exact match: main
	// - Method with params: main(String[])
	// - Qualified: Class.main(
	query := `
		SELECT ` + symbolColumns("s") + `,
		       c.file as call_file, c.line as call_line, c.column as call_column
		FROM symbols s
		JOIN calls c ON s.id = c.caller_id
		JOIN symbols callee ON c.callee_id = callee.id
		WHERE (callee.name = ? OR callee.name LIKE ? OR callee.name LIKE ?)`
	args := []interface{}{
		symbolName,               // Exact match
		symbolName + "(%",        // Method with params: main(
		"%." + symbolName + "(%", // Qualified with params: Class.main(
	}

	if len(languages) > 0 {
		query += " AND s.language IN (?" + repeatString(",?", len(languages)-1) + ")"
		for _, lang := range languages {
			args = append(args, lang)
		}
	}

	// One row per call site and caller: a site calling two matching callees
	// (an interface method and its implementation) names its caller once.
	query += " GROUP BY c.file, c.line, c.column, s.id ORDER BY c.file, c.line, c.column"

	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var callers []CallerInfo
	for rows.Next() {
		var c CallerInfo
		if err := rows.Scan(append(c.Symbol.scanTargets(), &c.CallFile, &c.CallLine, &c.CallColumn)...); err != nil {
			return nil, err
		}
		callers = append(callers, c)
	}
	return callers, rows.Err()
}

// GetCallees finds all callees of a symbol with call site info
func (m *Manager) GetCallees(symbolName string, languages []string) ([]CalleeInfo, error) {
	// Match caller names flexibly:
	// - Exact match: main
	// - Method with params: main(String[])
	// - Qualified: Class.main
	query := `
		SELECT ` + symbolColumns("s") + `,
		       c.file as call_file, c.line as call_line, c.column as call_column
		FROM symbols s
		JOIN calls c ON s.id = c.callee_id
		JOIN symbols caller ON c.caller_id = caller.id
		WHERE (caller.name = ? OR caller.name LIKE ? OR caller.name LIKE ?)`
	args := []interface{}{
		symbolName,               // Exact match
		symbolName + "(%",        // Method with params: main(
		"%." + symbolName + "(%", // Qualified with params: Class.main(
	}

	if len(languages) > 0 {
		query += " AND s.language IN (?" + repeatString(",?", len(languages)-1) + ")"
		for _, lang := range languages {
			args = append(args, lang)
		}
	}

	// One row per call site and callee: distinct targets at one site stay
	// distinct, and a caller matched twice does not repeat them.
	query += " GROUP BY c.file, c.line, c.column, s.id ORDER BY c.file, c.line, c.column"

	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var callees []CalleeInfo
	for rows.Next() {
		var c CalleeInfo
		if err := rows.Scan(append(c.Symbol.scanTargets(), &c.CallFile, &c.CallLine, &c.CallColumn)...); err != nil {
			return nil, err
		}
		callees = append(callees, c)
	}
	return callees, rows.Err()
}

// GetSignature finds the signature of a symbol
func (m *Manager) GetSignature(symbolName string, languages []string) ([]Symbol, error) {
	// Match symbol names flexibly:
	// - Exact match: main
	// - Method with params: main(String[])
	// - Qualified: Class.main
	query := `
		SELECT ` + symbolColumns("") + `
		FROM symbols
		WHERE (name = ? OR name LIKE ? OR name LIKE ?) AND signature IS NOT NULL AND signature != ''`
	args := []interface{}{
		symbolName,               // Exact match
		symbolName + "(%",        // Method with params: main(
		"%." + symbolName + "(%", // Qualified with params: Class.main(
	}

	if len(languages) > 0 {
		query += " AND language IN (?" + repeatString(",?", len(languages)-1) + ")"
		for _, lang := range languages {
			args = append(args, lang)
		}
	}

	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanSymbols(rows)
}

// GetFunctionSymbols returns all function symbols for a language
func (m *Manager) GetFunctionSymbols(language string) ([]Symbol, error) {
	query := `
		SELECT ` + symbolColumns("") + `
		FROM symbols
		WHERE kind IN ('function', 'method', 'constructor') AND language = ?
		ORDER BY file, line`

	rows, err := m.db.Query(query, language)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanSymbols(rows)
}

// GetTypeSymbols returns all class/interface/struct symbols for a language
func (m *Manager) GetTypeSymbols(language string) ([]Symbol, error) {
	query := `
		SELECT ` + symbolColumns("") + `
		FROM symbols
		WHERE kind IN ('class', 'interface', 'struct', 'type', 'enum') AND language = ?
		ORDER BY file, line`

	rows, err := m.db.Query(query, language)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanSymbols(rows)
}

// GetFileSymbols returns every symbol declared in file, in declaration order.
func (m *Manager) GetFileSymbols(file string) ([]Symbol, error) {
	rows, err := m.db.Query(`SELECT `+symbolColumns("")+` FROM symbols WHERE file = ? ORDER BY start_line, start_column`, file)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSymbols(rows)
}

// GetSymbolByName returns symbol by name (flexible matching)
func (m *Manager) GetSymbolByName(name string, languages []string) ([]Symbol, error) {
	// Match symbol names flexibly:
	// - Exact match: main
	// - Method with params: main(String[])
	// - Qualified: Class.main
	query := `
		SELECT ` + symbolColumns("") + `
		FROM symbols
		WHERE (name = ? OR name LIKE ? OR name LIKE ?)`
	args := []interface{}{
		name,               // Exact match
		name + "(%",        // Method with params: main(
		"%." + name + "(%", // Qualified with params: Class.main(
	}

	if len(languages) > 0 {
		query += " AND language IN (?" + repeatString(",?", len(languages)-1) + ")"
		for _, lang := range languages {
			args = append(args, lang)
		}
	}

	query += " ORDER BY file, line"

	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanSymbols(rows)
}

// GetStats is defined below with Stats struct

// RecordFileMeta upserts one file's refresh metadata: fingerprint,
// provider, source mtime and extraction time (R02).
func (m *Manager) RecordFileMeta(meta FileMeta) error {
	_, err := m.db.Exec(`
		INSERT OR REPLACE INTO file_meta (path, mod_time, language, content_hash, provider, extracted_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		meta.Path, meta.ModTime, meta.Language, meta.ContentHash, meta.Provider, meta.ExtractedAt,
	)
	return err
}

// ListFileMeta returns the published manifest: every indexed file with
// its language. It is what a refresh reconciles the current scan against,
// including on a legacy database whose narrower file_meta must still be
// readable so an explicit rebuild can replace it (R05, T23).
func (m *Manager) ListFileMeta() ([]FileMeta, error) {
	exists, err := m.tableExists("file_meta")
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	rows, err := m.db.Query("SELECT path, mod_time, language FROM file_meta")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var manifest []FileMeta
	for rows.Next() {
		var meta FileMeta
		if err := rows.Scan(&meta.Path, &meta.ModTime, &meta.Language); err != nil {
			return nil, err
		}
		manifest = append(manifest, meta)
	}
	return manifest, rows.Err()
}

// tableExists reports whether name is a table in the database.
func (m *Manager) tableExists(name string) (bool, error) {
	var count int
	if err := m.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetProviderDigests returns the stored provider digest per language: the
// configuration a snapshot was last extracted under. A database without
// the table — a legacy one — has none, so everything re-extracts.
func (m *Manager) GetProviderDigests() (map[string]string, error) {
	exists, err := m.tableExists("provider_state")
	if err != nil {
		return nil, err
	}
	if !exists {
		return map[string]string{}, nil
	}
	rows, err := m.db.Query("SELECT language, digest FROM provider_state")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	digests := map[string]string{}
	for rows.Next() {
		var language, digest string
		if err := rows.Scan(&language, &digest); err != nil {
			return nil, err
		}
		digests[language] = digest
	}
	return digests, rows.Err()
}

// GetFileMeta gets file metadata
func (m *Manager) GetFileMeta(path string) (*FileMeta, error) {
	var fm FileMeta
	err := m.db.QueryRow(
		"SELECT path, mod_time, language, content_hash, provider, extracted_at FROM file_meta WHERE path = ?",
		path,
	).Scan(&fm.Path, &fm.ModTime, &fm.Language, &fm.ContentHash, &fm.Provider, &fm.ExtractedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &fm, nil
}

// Stats holds database statistics
type Stats struct {
	SymbolCount int
	CallCount   int
	FileCount   int
	Languages   []string
}

// GetStats returns database statistics
func (m *Manager) GetStats() (*Stats, error) {
	stats := &Stats{}

	// Get symbol count
	err := m.db.QueryRow("SELECT COUNT(*) FROM symbols").Scan(&stats.SymbolCount)
	if err != nil {
		return nil, err
	}

	// Get call count
	err = m.db.QueryRow("SELECT COUNT(*) FROM calls").Scan(&stats.CallCount)
	if err != nil {
		return nil, err
	}

	// Get file count
	err = m.db.QueryRow("SELECT COUNT(DISTINCT file) FROM symbols").Scan(&stats.FileCount)
	if err != nil {
		return nil, err
	}

	// Get languages
	rows, err := m.db.Query("SELECT DISTINCT language FROM symbols")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var lang string
		if err := rows.Scan(&lang); err != nil {
			return nil, err
		}
		stats.Languages = append(stats.Languages, lang)
	}

	return stats, nil
}

// LanguageStats holds per-language statistics
type LanguageStats struct {
	Language string
	Count    int
	Percent  float64
}

// DetailedStats holds comprehensive database statistics
type DetailedStats struct {
	// Symbol counts by kind
	TotalSymbols int
	Functions    int
	Methods      int
	Classes      int
	Interfaces   int
	Structs      int
	Types        int
	Enums        int
	Variables    int
	Constants    int
	Modules      int

	// Call graph
	CallEdges int

	// Languages breakdown
	Languages []LanguageStats

	// Last build info
	LastBuildTime *time.Time
	FilesIndexed  int

	// Database info
	DatabasePath string
	DatabaseSize int64
}

// GetDetailedStats returns comprehensive database statistics
func (m *Manager) GetDetailedStats() (*DetailedStats, error) {
	stats := &DetailedStats{
		DatabasePath: m.dbPath,
	}

	// 1. Get total symbol count
	err := m.db.QueryRow("SELECT COUNT(*) FROM symbols").Scan(&stats.TotalSymbols)
	if err != nil {
		return nil, err
	}

	// 2. Get symbol counts grouped by kind
	kindRows, err := m.db.Query(`
		SELECT kind, COUNT(*) as count
		FROM symbols
		GROUP BY kind
	`)
	if err != nil {
		return nil, err
	}
	defer kindRows.Close()

	for kindRows.Next() {
		var kind string
		var count int
		if err := kindRows.Scan(&kind, &count); err != nil {
			return nil, err
		}
		switch kind {
		case "function":
			stats.Functions = count
		case "method":
			stats.Methods = count
		case "class":
			stats.Classes = count
		case "interface":
			stats.Interfaces = count
		case "struct":
			stats.Structs = count
		case "type":
			stats.Types = count
		case "enum":
			stats.Enums = count
		case "variable":
			stats.Variables = count
		case "constant":
			stats.Constants = count
		case "module":
			stats.Modules = count
		}
	}

	// 3. Get call edge count
	err = m.db.QueryRow("SELECT COUNT(*) FROM calls").Scan(&stats.CallEdges)
	if err != nil {
		return nil, err
	}

	// 4. Get language breakdown with percentages
	langRows, err := m.db.Query(`
		SELECT language, COUNT(*) as count
		FROM symbols
		GROUP BY language
		ORDER BY count DESC
	`)
	if err != nil {
		return nil, err
	}
	defer langRows.Close()

	for langRows.Next() {
		var lang string
		var count int
		if err := langRows.Scan(&lang, &count); err != nil {
			return nil, err
		}
		percent := float64(count) / float64(stats.TotalSymbols) * 100
		stats.Languages = append(stats.Languages, LanguageStats{
			Language: lang,
			Count:    count,
			Percent:  percent,
		})
	}

	// 5. Get last build time (max mod_time from file_meta)
	var lastBuildStr sql.NullString
	err = m.db.QueryRow("SELECT MAX(mod_time) FROM file_meta").Scan(&lastBuildStr)
	if err != nil {
		return nil, err
	}
	if lastBuildStr.Valid {
		// Parse RFC3339 format
		lastBuildTime, err := time.Parse(time.RFC3339, lastBuildStr.String)
		if err == nil {
			stats.LastBuildTime = &lastBuildTime
		}
	}

	// 6. Get files indexed count
	err = m.db.QueryRow("SELECT COUNT(*) FROM file_meta").Scan(&stats.FilesIndexed)
	if err != nil {
		return nil, err
	}

	// 7. Get database file size
	if info, err := os.Stat(m.dbPath); err == nil {
		stats.DatabaseSize = info.Size()
	}

	return stats, nil
}

// Helper functions

// symbolColumns is the column list every symbol read and write uses, in the
// order of Symbol.scanTargets; alias qualifies each column when non-empty.
func symbolColumns(alias string) string {
	columns := []string{"id", "name", "kind", "file", "line", "column", "start_line", "start_column",
		"end_line", "end_column", "scope", "signature", "documentation", "language", "source", "created_at"}
	if alias != "" {
		for i, column := range columns {
			columns[i] = alias + "." + column
		}
	}
	return strings.Join(columns, ", ")
}

func (s *Symbol) scanTargets() []any {
	return []any{&s.ID, &s.Name, &s.Kind, &s.File, &s.Line, &s.Column, &s.StartLine, &s.StartColumn,
		&s.EndLine, &s.EndColumn, &s.Scope, &s.Signature, &s.Documentation, &s.Language, &s.Source, &s.CreatedAt}
}

func scanSymbols(rows *sql.Rows) ([]Symbol, error) {
	var symbols []Symbol
	for rows.Next() {
		var s Symbol
		if err := rows.Scan(s.scanTargets()...); err != nil {
			return nil, err
		}
		symbols = append(symbols, s)
	}
	return symbols, rows.Err()
}

func repeatString(s string, n int) string {
	result := ""
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}
