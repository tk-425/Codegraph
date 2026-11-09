package indexer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tk-425/Codegraph/internal/db"
	"github.com/tk-425/Codegraph/internal/lsp"
)

// CallGraphIndexer handles call hierarchy extraction using references
type CallGraphIndexer struct {
	db       *db.Manager
	mgr      *lsp.Manager
	rootPath string
	// failures counts failed requests of the latest IndexCallGraph call.
	failures requestFailures
}

// NewCallGraphIndexer creates a new call graph indexer
func NewCallGraphIndexer(dbManager *db.Manager, lspManager *lsp.Manager, rootPath string) *CallGraphIndexer {
	return &CallGraphIndexer{
		db:       dbManager,
		mgr:      lspManager,
		rootPath: rootPath,
	}
}

// IndexCallGraph extracts call relationships using textDocument/references
// For each function symbol, we find all references to it - these are potential call sites
func (c *CallGraphIndexer) IndexCallGraph(ctx context.Context, language string) (int, error) {
	c.failures = requestFailures{}
	// Get LSP client for this language
	client, err := c.mgr.GetClient(ctx, language)
	if err != nil {
		return 0, fmt.Errorf("failed to get LSP client: %w", err)
	}

	// Clear existing call graph for this language to avoid duplicates
	if err := c.db.ClearCalls(language); err != nil {
		return 0, fmt.Errorf("failed to clear existing calls: %w", err)
	}

	// Get all function symbols from database
	symbols, err := c.db.GetFunctionSymbols(language)
	if err != nil {
		return 0, fmt.Errorf("failed to get function symbols: %w", err)
	}

	callCount := 0
	openedFiles := make(map[string]bool)
	root := lsp.NewRoot(c.rootPath)
	lines := lsp.NewSourceLines()
	enc := client.PositionEncoding()

	for _, sym := range symbols {
		fileURI := lsp.PathToURI(sym.File)

		// Open file if not already opened
		if !openedFiles[fileURI] {
			content, err := readFileContent(sym.File)
			if err != nil {
				continue
			}
			if err := client.DidOpenTextDocument(fileURI, language, content); err != nil {
				c.failures.add(client, err)
				continue
			}
			openedFiles[fileURI] = true
		}

		// Get position of the function name
		pos := lines.ToLSP(enc, sym.File, sym.Line, sym.Column)

		// Find all references to this function (excluding declaration)
		refs, err := client.References(ctx, fileURI, pos, false)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return callCount, ctxErr
			}
			c.failures.add(client, err)
			continue
		}

		// Each reference is a potential call site
		for _, ref := range refs {
			refPath, err := root.Path(ref.URI)
			if err != nil {
				continue
			}
			
			refLine, refColumn := lines.FromLSP(enc, refPath, ref.Range.Start)

			// Skip if same location as declaration
			if refPath == sym.File && refLine == sym.Line {
				continue
			}

			// Find which function contains this reference
			callerID := c.findContainingFunction(refPath, refLine, language)
			if callerID == "" {
				continue
			}

			// Store call relationship
			dbCall := &db.Call{
				CallerID: callerID,
				CalleeID: sym.ID,
				File:     refPath,
				Line:     refLine,
				Column:   refColumn,
			}

			if err := c.db.InsertCall(dbCall); err != nil {
				// Skip duplicate calls
				continue
			}
			callCount++
		}
	}

	// Close opened files
	for fileURI := range openedFiles {
		client.DidCloseTextDocument(fileURI)
	}

	return callCount, nil
}

// findContainingFunction finds which function contains a given line
func (c *CallGraphIndexer) findContainingFunction(file string, line int, language string) string {
	// Query database for function that spans this line
	symbols, err := c.db.GetFunctionSymbols(language)
	if err != nil {
		return ""
	}

	// Normalize file path for comparison
	absFile, _ := filepath.Abs(file)

	for _, sym := range symbols {
		absSymFile, _ := filepath.Abs(sym.File)
		if absSymFile != absFile {
			continue
		}

		// Check if line is within function range
		if sym.EndLine != nil {
			if line >= sym.Line && line <= *sym.EndLine {
				return sym.ID
			}
		} else {
			// If no end line, just check if after start
			if line >= sym.Line {
				return sym.ID
			}
		}
	}

	return ""
}

// Helper functions

func readFileContent(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
