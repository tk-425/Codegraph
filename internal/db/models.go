package db

import "time"

// Symbol represents a code symbol (function, class, variable, etc.)
type Symbol struct {
	ID            string    `json:"id"`            // Unique ID: "path/file.go#Scope.Name"
	Name          string    `json:"name"`          // Symbol name
	Kind          string    `json:"kind"`          // function, variable, class, interface, type, module
	File          string    `json:"file"`          // File path
	Line          int       `json:"line"`          // Name line (1-indexed)
	Column        int       `json:"column"`        // Name column: 0-indexed UTF-8 byte offset
	StartLine     int       `json:"start_line"`    // Declaration start line (1-indexed)
	StartColumn   int       `json:"start_column"`  // Declaration start column (UTF-8 byte offset)
	EndLine       *int      `json:"end_line"`      // End line (optional)
	EndColumn     *int      `json:"end_column"`    // End column (optional)
	Scope         string    `json:"scope"`         // Parent scope
	Signature     string    `json:"signature"`     // Function signature
	Documentation string    `json:"documentation"` // Documentation/comments
	Language      string    `json:"language"`      // Programming language
	Source        string    `json:"source"`        // lsp, tree-sitter, ast-grep, ripgrep
	CreatedAt     time.Time `json:"created_at"`    // When indexed
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

// CallerInfo combines caller symbol info with call site location
type CallerInfo struct {
	Symbol            // Embedded caller symbol
	CallFile   string `json:"call_file"`   // File where call occurs
	CallLine   int    `json:"call_line"`   // Line of call site
	CallColumn int    `json:"call_column"` // Column of call site
}

// CalleeInfo combines callee symbol info with call site location
type CalleeInfo struct {
	Symbol            // Embedded callee symbol
	CallFile   string `json:"call_file"`   // File where call occurs
	CallLine   int    `json:"call_line"`   // Line of call site
	CallColumn int    `json:"call_column"` // Column of call site
}

// TypeHierarchy represents a type relationship (extends, implements)
type TypeHierarchy struct {
	ID           int64  `json:"id"`
	ChildID      string `json:"child_id"`     // Subclass/implementor
	ParentID     string `json:"parent_id"`    // Superclass/interface
	Relationship string `json:"relationship"` // "extends" or "implements"
}

// FileMeta stores file metadata for incremental builds
type FileMeta struct {
	Path     string    `json:"path"`
	ModTime  time.Time `json:"mod_time"`
	Language string    `json:"language"`
}

// Unresolved is an observed relationship site whose target could not be
// identified safely: no candidate ("unresolved"), several ("ambiguous"), a
// target outside the project ("external"), or a reference with no
// invocation evidence ("unvalidated").
type Unresolved struct {
	Kind       string `json:"kind"` // call, extends, implements, embeds
	Language   string `json:"language"`
	FromID     string `json:"from_id"` // the resolved local endpoint, if any
	TargetName string `json:"target_name"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Column     int    `json:"column"`
	Reason     string `json:"reason"`
	Candidates int    `json:"candidates"`
}
