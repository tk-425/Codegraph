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

// SearchSymbols searches for symbols by name with optional filters
func (m *Manager) SearchSymbols(name string, kind string, languages []string) ([]Symbol, error) {
	query := "SELECT id, name, kind, file, line, column, end_line, end_column, scope, signature, documentation, language, source, created_at FROM symbols WHERE name LIKE ?"
	args := []interface{}{"%" + name + "%"}

	if kind != "" {
		query += " AND kind = ?"
		args = append(args, kind)
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

// GetCallers finds all callers of a symbol
func (m *Manager) GetCallers(symbolName string, languages []string) ([]Symbol, error) {
	// Join calls table to find caller symbols
	// callee_id format is path#name (e.g., internal/db/manager.go#NewManager)
	query := `
		SELECT DISTINCT s.id, s.name, s.kind, s.file, s.line, s.column, s.end_line, s.end_column, 
		       s.scope, s.signature, s.documentation, s.language, s.source, s.created_at
		FROM symbols s
		JOIN calls c ON s.id = c.caller_id
		WHERE c.callee_id LIKE ?`
	args := []interface{}{"%#" + symbolName}

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

// GetCallees finds all callees of a symbol
func (m *Manager) GetCallees(symbolName string, languages []string) ([]Symbol, error) {
	// First get calls where caller matches
	query := `
		SELECT DISTINCT s.id, s.name, s.kind, s.file, s.line, s.column, s.end_line, s.end_column, 
		       s.scope, s.signature, s.documentation, s.language, s.source, s.created_at
		FROM symbols s
		WHERE s.id IN (
			SELECT c.callee_id FROM calls c
			JOIN symbols caller ON c.caller_id = caller.id
			WHERE caller.name = ?
		)`
	args := []interface{}{symbolName}

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

// GetSignature finds the signature of a symbol
func (m *Manager) GetSignature(symbolName string, languages []string) ([]Symbol, error) {
	query := `
		SELECT id, name, kind, file, line, column, end_line, end_column, scope, signature, documentation, language, source, created_at
		FROM symbols
		WHERE name = ? AND signature IS NOT NULL AND signature != ''`
	args := []interface{}{symbolName}

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

// GetSymbolByName returns symbol by exact name match
func (m *Manager) GetSymbolByName(name string, languages []string) ([]Symbol, error) {
	query := `
		SELECT id, name, kind, file, line, column, end_line, end_column, scope, signature, documentation, language, source, created_at
		FROM symbols
		WHERE name = ?`
	args := []interface{}{name}

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

// GetStats returns database statistics
func (m *Manager) GetStats() (map[string]int64, error) {
	stats := make(map[string]int64)
	tables := []string{"symbols", "calls", "type_hierarchy", "file_meta"}

	for _, table := range tables {
		var count int64
		err := m.db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count)
		if err != nil {
			return nil, err
		}
		stats[table] = count
	}

	return stats, nil
}

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
