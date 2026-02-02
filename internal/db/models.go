package db

import "time"

// Symbol represents a code symbol (function, class, variable, etc.)
type Symbol struct {
	ID            string    `json:"id"`             // Unique ID: "path/file.go#Scope.Name"
	Name          string    `json:"name"`           // Symbol name
	Kind          string    `json:"kind"`           // function, variable, class, interface, type, module
	File          string    `json:"file"`           // File path
	Line          int       `json:"line"`           // Line number (1-indexed)
	Column        int       `json:"column"`         // Column number (0-indexed)
	EndLine       *int      `json:"end_line"`       // End line (optional)
	EndColumn     *int      `json:"end_column"`     // End column (optional)
	Scope         string    `json:"scope"`          // Parent scope
	Signature     string    `json:"signature"`      // Function signature
	Documentation string    `json:"documentation"`  // Documentation/comments
	Language      string    `json:"language"`       // Programming language
	Source        string    `json:"source"`         // lsp, tree-sitter, ast-grep, ripgrep
	CreatedAt     time.Time `json:"created_at"`     // When indexed
}

// Call represents a call relationship between symbols
type Call struct {
	ID       int64  `json:"id"`
	CallerID string `json:"caller_id"` // Symbol that makes the call
	CalleeID string `json:"callee_id"` // Symbol being called
	File     string `json:"file"`      // File where call occurs
	Line     int    `json:"line"`      // Line of call
	Column   int    `json:"column"`    // Column of call
}

// TypeHierarchy represents a type relationship (extends, implements)
type TypeHierarchy struct {
	ID           int64  `json:"id"`
	ChildID      string `json:"child_id"`      // Subclass/implementor
	ParentID     string `json:"parent_id"`     // Superclass/interface
	Relationship string `json:"relationship"`  // "extends" or "implements"
}

// FileMeta stores file metadata for incremental builds
type FileMeta struct {
	Path     string    `json:"path"`
	ModTime  time.Time `json:"mod_time"`
	Language string    `json:"language"`
}
