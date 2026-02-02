package indexer

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/tk-425/Codegraph/internal/config"
	"github.com/tk-425/Codegraph/internal/db"
	"github.com/tk-425/Codegraph/internal/lsp"
)

// Indexer handles symbol extraction and storage
type Indexer struct {
	cfg      *config.Config
	db       *db.Manager
	lsp      *lsp.Manager
	rootPath string
	rootURI  string
}

// NewIndexer creates a new indexer
func NewIndexer(cfg *config.Config, dbManager *db.Manager, rootPath string) *Indexer {
	absPath, _ := filepath.Abs(rootPath)
	rootURI := "file://" + absPath

	return &Indexer{
		cfg:      cfg,
		db:       dbManager,
		lsp:      lsp.NewManager(cfg, rootURI),
		rootPath: absPath,
		rootURI:  rootURI,
	}
}

// IndexProject indexes all source files in the project
func (i *Indexer) IndexProject(ctx context.Context, files []FileInfo, force bool) error {
	if force {
		if err := i.db.ClearAll(); err != nil {
			return fmt.Errorf("failed to clear database: %w", err)
		}
	}

	// Group files by language
	groups := GroupByLanguage(files)

	indexedFiles := 0
	skippedFiles := 0
	totalSymbols := 0

	for language, langFiles := range groups {
		langTotal := len(langFiles)
		langIndexed := 0
		langSkipped := 0
		langLSP := 0
		langTreeSitter := 0

		// Get LSP client for this language
		client, err := i.lsp.GetClient(ctx, language)
		if err != nil {
			fmt.Printf("   ⚠️  Skipping %s: %v\n", language, err)
			continue
		}

		// Some LSP servers need time to analyze the project after initialization
		switch language {
		case "rust":
			time.Sleep(10 * time.Second)
		case "java":
			time.Sleep(10 * time.Second)
		case "swift":
			time.Sleep(10 * time.Second)
		case "ocaml":
			time.Sleep(10 * time.Second)
		}

		for idx, file := range langFiles {
			// Check if file needs re-indexing (incremental build)
			if !force {
				if skip, _ := i.shouldSkipFile(file); skip {
					langSkipped++
					skippedFiles++
					continue
				}
			}

			// Show progress
			progress := float64(idx+1) / float64(langTotal) * 100
			fmt.Printf("\r   [%s] %d/%d files (%.0f%%) ", language, idx+1, langTotal, progress)

			symbols, err := i.indexFile(ctx, client, file)
			if err != nil {
				// Try tree-sitter fallback
				tsIndexer := NewTreeSitterIndexer(i.db, i.rootPath)
				symbols, tsErr := tsIndexer.IndexFile(ctx, file)
				if tsErr != nil {
					fmt.Printf("\n   ⚠️  Error indexing %s: %v (tree-sitter: %v)\n", file.RelPath, err, tsErr)
					continue
				}
				// Tree-sitter succeeded
				langIndexed++
				langTreeSitter++
				indexedFiles++
				totalSymbols += symbols
				continue
			}

			langIndexed++
			langLSP++
			indexedFiles++
			totalSymbols += symbols
		}

		// Clear progress line and show summary with source counts
		if langIndexed > 0 {
			fmt.Printf("\r   [%s] %d indexed (%d LSP, %d tree-sitter), %d skipped         \n", language, langIndexed, langLSP, langTreeSitter, langSkipped)
		} else if langSkipped > 0 {
			fmt.Printf("\r   [%s] 0 indexed, %d skipped (unchanged)         \n", language, langSkipped)
		}
	}

	// Index call graph for each language
	fmt.Println("📊 Extracting call graph (via references)...")
	callGraphIndexer := NewCallGraphIndexer(i.db, i.lsp, i.rootPath)
	totalCalls := 0
	for language := range groups {
		calls, err := callGraphIndexer.IndexCallGraph(ctx, language)
		if err != nil {
			fmt.Printf("   ⚠️  Call graph error for %s: %v\n", language, err)
			continue
		}
		totalCalls += calls
	}
	fmt.Printf("   Found %d call relationships\n", totalCalls)

	// Shutdown LSP servers
	i.lsp.ShutdownAll()

	fmt.Printf("✅ Indexed %d files, skipped %d unchanged, %d symbols, %d calls\n",
		indexedFiles, skippedFiles, totalSymbols, totalCalls)
	return nil
}

// shouldSkipFile checks if file is unchanged since last index
func (i *Indexer) shouldSkipFile(file FileInfo) (bool, error) {
	// Get file's current modification time
	stat, err := os.Stat(file.Path)
	if err != nil {
		return false, err
	}
	currentMtime := stat.ModTime()

	// Get stored metadata
	meta, err := i.db.GetFileMeta(file.Path)
	if err != nil {
		return false, err
	}

	// If no metadata, file hasn't been indexed before
	if meta == nil {
		return false, nil
	}

	// Skip if file hasn't changed
	return !currentMtime.After(meta.ModTime), nil
}

// indexFile indexes a single file and returns number of symbols stored
func (i *Indexer) indexFile(ctx context.Context, client *lsp.Client, file FileInfo) (int, error) {
	// Convert path to URI
	fileURI := pathToURI(file.Path)

	// Get document symbols from LSP
	symbols, err := client.DocumentSymbols(ctx, fileURI)
	if err != nil {
		return 0, err
	}

	// Store symbols in database
	count := 0
	if err := i.storeSymbols(file, symbols, "", &count); err != nil {
		return 0, err
	}

	// Update file metadata
	if err := i.db.UpdateFileMeta(file.Path, time.Now(), file.Language); err != nil {
		return 0, err
	}

	return count, nil
}

// storeSymbols recursively stores symbols in the database
func (i *Indexer) storeSymbols(file FileInfo, symbols []lsp.DocumentSymbol, scope string, count *int) error {
	for _, sym := range symbols {
		// Create symbol ID
		id := fmt.Sprintf("%s#%s", file.RelPath, sym.Name)
		if scope != "" {
			id = fmt.Sprintf("%s#%s.%s", file.RelPath, scope, sym.Name)
		}

		// Create database symbol
		dbSym := &db.Symbol{
			ID:            id,
			Name:          sym.Name,
			Kind:          lsp.SymbolKindToString(sym.Kind),
			File:          file.Path,
			Line:          sym.SelectionRange.Start.Line + 1, // LSP is 0-indexed
			Column:        sym.SelectionRange.Start.Character,
			EndLine:       intPtr(sym.Range.End.Line + 1),
			EndColumn:     intPtr(sym.Range.End.Character),
			Scope:         scope,
			Signature:     sym.Detail,
			Documentation: "",
			Language:      file.Language,
			Source:        "lsp",
			CreatedAt:     time.Now(),
		}

		if err := i.db.InsertSymbol(dbSym); err != nil {
			return err
		}
		*count++

		// Recursively process children
		if len(sym.Children) > 0 {
			childScope := sym.Name
			if scope != "" {
				childScope = scope + "." + sym.Name
			}
			if err := i.storeSymbols(file, sym.Children, childScope, count); err != nil {
				return err
			}
		}
	}

	return nil
}

// Close shuts down all LSP servers
func (i *Indexer) Close() {
	i.lsp.ShutdownAll()
}

// Helper functions

func pathToURI(path string) string {
	absPath, _ := filepath.Abs(path)
	u := url.URL{
		Scheme: "file",
		Path:   absPath,
	}
	return u.String()
}

func intPtr(i int) *int {
	return &i
}
