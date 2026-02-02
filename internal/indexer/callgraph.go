package indexer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tk-425/Codegraph/internal/db"
	"github.com/tk-425/Codegraph/internal/lsp"
)

// CallGraphIndexer handles call hierarchy extraction using references
type CallGraphIndexer struct {
	db       *db.Manager
	mgr      *lsp.Manager
	rootPath string
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
	// Get LSP client for this language
	client, err := c.mgr.GetClient(ctx, language)
	if err != nil {
		return 0, fmt.Errorf("failed to get LSP client: %w", err)
	}

	// Get all function symbols from database
	symbols, err := c.db.GetFunctionSymbols(language)
	if err != nil {
		return 0, fmt.Errorf("failed to get function symbols: %w", err)
	}

	callCount := 0
	openedFiles := make(map[string]bool)

	for _, sym := range symbols {
		fileURI := "file://" + sym.File

		// Open file if not already opened
		if !openedFiles[fileURI] {
			content, err := readFileContent(sym.File)
			if err != nil {
				continue
			}
			if err := client.DidOpenTextDocument(fileURI, language, content); err != nil {
				continue
			}
			openedFiles[fileURI] = true
		}

		// Get position of the function name
		pos := lsp.Position{
			Line:      sym.Line - 1, // Convert to 0-indexed
			Character: sym.Column,
		}

		// Find all references to this function (excluding declaration)
		refs, err := client.References(ctx, fileURI, pos, false)
		if err != nil {
			continue
		}

		// Each reference is a potential call site
		for _, ref := range refs {
			refPath := uriToPath(ref.URI)
			
			// Skip if same location as declaration
			if refPath == sym.File && ref.Range.Start.Line+1 == sym.Line {
				continue
			}

			// Find which function contains this reference
			callerID := c.findContainingFunction(refPath, ref.Range.Start.Line+1, language)
			if callerID == "" {
				continue
			}

			// Store call relationship
			dbCall := &db.Call{
				CallerID: callerID,
				CalleeID: sym.ID,
				File:     refPath,
				Line:     ref.Range.Start.Line + 1,
				Column:   ref.Range.Start.Character,
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

func uriToPath(uri string) string {
	if strings.HasPrefix(uri, "file://") {
		return uri[7:]
	}
	return uri
}

func readFileContent(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
