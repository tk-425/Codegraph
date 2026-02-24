package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Manager handles database operations
type Manager struct {
	db     *sql.DB
	dbPath string
}

// NewManager creates a new database manager
func NewManager(dbPath string) (*Manager, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	return &Manager{db: db, dbPath: dbPath}, nil
}

// Initialize creates all tables and indexes
func (m *Manager) Initialize() error {
	for _, stmt := range AllSchemaStatements() {
		if _, err := m.db.Exec(stmt); err != nil {
			return fmt.Errorf("failed to execute schema statement: %w", err)
		}
	}
	return nil
}

// Close closes the database connection
func (m *Manager) Close() error {
	return m.db.Close()
}

// ClearAll deletes all data (for full rebuild)
func (m *Manager) ClearAll() error {
	tables := []string{"calls", "type_hierarchy", "symbols", "file_meta"}
	for _, table := range tables {
		if _, err := m.db.Exec(fmt.Sprintf("DELETE FROM %s", table)); err != nil {
			return fmt.Errorf("failed to clear %s: %w", table, err)
		}
	}
	return nil
}

// ClearCalls deletes all calls for a specific language
func (m *Manager) ClearCalls(language string) error {
	query := `
		DELETE FROM calls 
		WHERE caller_id IN (
			SELECT id FROM symbols WHERE language = ?
		)`

	if _, err := m.db.Exec(query, language); err != nil {
		return fmt.Errorf("failed to clear calls for %s: %w", language, err)
	}
	return nil
}

// ClearTypeHierarchy deletes all type hierarchy for a specific language
func (m *Manager) ClearTypeHierarchy(language string) error {
	query := `
		DELETE FROM type_hierarchy 
		WHERE child_id IN (
			SELECT id FROM symbols WHERE language = ?
		)`

	if _, err := m.db.Exec(query, language); err != nil {
		return fmt.Errorf("failed to clear type hierarchy for %s: %w", language, err)
	}
	return nil
}

// InsertSymbol inserts a symbol into the database
func (m *Manager) InsertSymbol(s *Symbol) error {
	_, err := m.db.Exec(`
		INSERT OR REPLACE INTO symbols 
		(id, name, kind, file, line, column, end_line, end_column, scope, signature, documentation, language, source, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.Name, s.Kind, s.File, s.Line, s.Column, s.EndLine, s.EndColumn,
		s.Scope, s.Signature, s.Documentation, s.Language, s.Source, s.CreatedAt,
	)
	return err
}

// InsertCall inserts a call relationship
func (m *Manager) InsertCall(c *Call) error {
	_, err := m.db.Exec(`
		INSERT INTO calls (caller_id, callee_id, file, line, column)
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

// GetImplementations returns symbols that implement/extend the given parent symbol
func (m *Manager) GetImplementations(parentID string) ([]Symbol, error) {
	query := `
		SELECT s.id, s.name, s.kind, s.file, s.line, s.column, s.end_line, s.end_column, 
			   s.scope, s.signature, s.documentation, s.language, s.source, s.created_at
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

// GetImplementationsByName returns symbols that implement/extend a type by its name
func (m *Manager) GetImplementationsByName(typeName string) ([]Symbol, error) {
	query := `
		SELECT s.id, s.name, s.kind, s.file, s.line, s.column, s.end_line, s.end_column, 
			   s.scope, s.signature, s.documentation, s.language, s.source, s.created_at
		FROM symbols s
		INNER JOIN type_hierarchy th ON s.id = th.child_id
		INNER JOIN symbols parent ON th.parent_id = parent.id
		WHERE parent.name = ?
		ORDER BY s.file, s.line`

	rows, err := m.db.Query(query, typeName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanSymbols(rows)
}

// SearchSymbols searches for symbols by name with optional filters
func (m *Manager) SearchSymbols(name string, kind string, languages []string) ([]Symbol, error) {
	query := "SELECT id, name, kind, file, line, column, end_line, end_column, scope, signature, documentation, language, source, created_at FROM symbols WHERE name LIKE ?"
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
	// Join calls table to find caller symbols
	// callee_id format varies:
	// - Go: path#FunctionName
	// - Java: path#Class.methodName(params)
	// - C#: path#ClassName.MethodName
	// We need to match when symbolName appears after # or after . (for method names)
	query := `
		SELECT s.id, s.name, s.kind, s.file, s.line, s.column, s.end_line, s.end_column, 
		       s.scope, s.signature, s.documentation, s.language, s.source, s.created_at,
		       c.file as call_file, c.line as call_line, c.column as call_column
		FROM symbols s
		JOIN calls c ON s.id = c.caller_id
		WHERE (c.callee_id LIKE ? OR c.callee_id LIKE ? OR c.callee_id LIKE ?)`
	// Match: #symbolName, #Class.symbolName, or .symbolName(
	args := []interface{}{
		"%#" + symbolName,          // Exact function: path#FunctionName
		"%#%." + symbolName + "(%", // Method with params: path#Class.method(
		"%." + symbolName,          // Method without params: path#Class.method
	}

	if len(languages) > 0 {
		query += " AND s.language IN (?" + repeatString(",?", len(languages)-1) + ")"
		for _, lang := range languages {
			args = append(args, lang)
		}
	}

	// Group by call site to avoid duplicates when multiple callees match (e.g., interface + impl)
	query += " GROUP BY c.file, c.line, c.column ORDER BY c.file, c.line"

	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var callers []CallerInfo
	for rows.Next() {
		var c CallerInfo
		var endLine, endColumn *int
		err := rows.Scan(
			&c.ID, &c.Name, &c.Kind, &c.File, &c.Line, &c.Column,
			&endLine, &endColumn, &c.Scope, &c.Signature, &c.Documentation,
			&c.Language, &c.Source, &c.CreatedAt,
			&c.CallFile, &c.CallLine, &c.CallColumn,
		)
		if err != nil {
			return nil, err
		}
		c.EndLine = endLine
		c.EndColumn = endColumn
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
		SELECT s.id, s.name, s.kind, s.file, s.line, s.column, s.end_line, s.end_column, 
		       s.scope, s.signature, s.documentation, s.language, s.source, s.created_at,
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

	// Group by call site to deduplicate (interface + impl at same line)
	query += " GROUP BY c.file, c.line, c.column ORDER BY c.file, c.line"

	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var callees []CalleeInfo
	for rows.Next() {
		var c CalleeInfo
		var endLine, endColumn *int
		err := rows.Scan(
			&c.ID, &c.Name, &c.Kind, &c.File, &c.Line, &c.Column,
			&endLine, &endColumn, &c.Scope, &c.Signature, &c.Documentation,
			&c.Language, &c.Source, &c.CreatedAt,
			&c.CallFile, &c.CallLine, &c.CallColumn,
		)
		if err != nil {
			return nil, err
		}
		c.EndLine = endLine
		c.EndColumn = endColumn
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
		SELECT id, name, kind, file, line, column, end_line, end_column, scope, signature, documentation, language, source, created_at
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
		SELECT id, name, kind, file, line, column, end_line, end_column, scope, signature, documentation, language, source, created_at
		FROM symbols
		WHERE kind IN ('function', 'method') AND language = ?
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
		SELECT id, name, kind, file, line, column, end_line, end_column, scope, signature, documentation, language, source, created_at
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

// GetSymbolByName returns symbol by name (flexible matching)
func (m *Manager) GetSymbolByName(name string, languages []string) ([]Symbol, error) {
	// Match symbol names flexibly:
	// - Exact match: main
	// - Method with params: main(String[])
	// - Qualified: Class.main
	query := `
		SELECT id, name, kind, file, line, column, end_line, end_column, scope, signature, documentation, language, source, created_at
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

// UpdateFileMeta updates file metadata for incremental builds
func (m *Manager) UpdateFileMeta(path string, modTime time.Time, language string) error {
	_, err := m.db.Exec(`
		INSERT OR REPLACE INTO file_meta (path, mod_time, language)
		VALUES (?, ?, ?)`,
		path, modTime, language,
	)
	return err
}

// GetFileMeta gets file metadata
func (m *Manager) GetFileMeta(path string) (*FileMeta, error) {
	var fm FileMeta
	err := m.db.QueryRow(
		"SELECT path, mod_time, language FROM file_meta WHERE path = ?",
		path,
	).Scan(&fm.Path, &fm.ModTime, &fm.Language)

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

func scanSymbols(rows *sql.Rows) ([]Symbol, error) {
	var symbols []Symbol
	for rows.Next() {
		var s Symbol
		err := rows.Scan(
			&s.ID, &s.Name, &s.Kind, &s.File, &s.Line, &s.Column,
			&s.EndLine, &s.EndColumn, &s.Scope, &s.Signature,
			&s.Documentation, &s.Language, &s.Source, &s.CreatedAt,
		)
		if err != nil {
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
