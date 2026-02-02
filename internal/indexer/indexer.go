package indexer

import (
	"context"
	"fmt"
	"net/url"
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

	totalFiles := len(files)
	indexedFiles := 0
	totalSymbols := 0

	for language, langFiles := range groups {
		fmt.Printf("   [%s] Indexing %d files...\n", language, len(langFiles))

		// Get LSP client for this language
		client, err := i.lsp.GetClient(ctx, language)
		if err != nil {
			fmt.Printf("   ⚠️  Skipping %s: %v\n", language, err)
			continue
		}

		for _, file := range langFiles {
			symbols, err := i.indexFile(ctx, client, file)
			if err != nil {
				fmt.Printf("   ⚠️  Error indexing %s: %v\n", file.RelPath, err)
				continue
			}

			indexedFiles++
			totalSymbols += symbols
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

	fmt.Printf("✅ Indexed %d/%d files, %d symbols, %d calls\n", indexedFiles, totalFiles, totalSymbols, totalCalls)
	return nil
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
