package indexer

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tk-425/Codegraph/internal/config"
	"github.com/tk-425/Codegraph/internal/db"
	"github.com/tk-425/Codegraph/internal/lsp"
)

// Indexer handles symbol extraction and storage
type Indexer struct {
	cfg         *config.Config
	db          *db.Manager
	lsp         *lsp.Manager
	rootPath    string
	rootURI     string
	diagnostics []lsp.LSPDiagnostic
	// phaseDiagnostics holds request failures attributed to a phase during
	// the current IndexProject call.
	phaseDiagnostics []lsp.LSPDiagnostic
	progress         io.Writer
}

// requestFailures counts the failed LSP requests of one phase and language.
// Unsupported-method replies are not failures and are not counted.
type requestFailures struct {
	executable string
	count      int
	first      error
}

func (f *requestFailures) add(client *lsp.Client, err error) {
	if lsp.IsUnsupported(err) {
		return
	}
	if f.count == 0 {
		f.executable = client.Executable()
		f.first = err
	}
	f.count++
}

// recordRequestFailures keeps failed requests visible even when other
// requests in the phase succeeded.
func (i *Indexer) recordRequestFailures(phase, language string, failures requestFailures) {
	if failures.count == 0 {
		return
	}
	i.phaseDiagnostics = append(i.phaseDiagnostics, lsp.LSPDiagnostic{
		Language:   language,
		Executable: failures.executable,
		Category:   lsp.RequestFailed,
		Severity:   lsp.SeverityWarning,
		Reason:     fmt.Sprintf("%s phase: %d LSP request(s) failed; affected results used fallback or were omitted; first error: %v", phase, failures.count, failures.first),
	})
}

// serverSettleDelay is a fixed pause some servers need after initialization
// before answering for the whole project. It is not readiness detection.
var serverSettleDelay = map[string]time.Duration{
	"rust":  10 * time.Second,
	"java":  10 * time.Second,
	"swift": 10 * time.Second,
	"ocaml": 10 * time.Second,
}

func waitForServerSettle(ctx context.Context, language string) error {
	delay, ok := serverSettleDelay[language]
	if !ok {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// NewIndexer creates a new indexer
func NewIndexer(cfg *config.Config, dbManager *db.Manager, rootPath string) *Indexer {
	absPath, _ := filepath.Abs(rootPath)
	rootURI := lsp.NewRoot(absPath).URI()

	return &Indexer{
		cfg:         cfg,
		db:          dbManager,
		lsp:         lsp.NewManager(cfg, rootURI),
		rootPath:    absPath,
		rootURI:     rootURI,
		diagnostics: nil,
		progress:    os.Stdout,
	}
}

// SetProgressWriter redirects the indexer's human-readable progress output.
// Callers emitting machine-readable output pass io.Discard so stdout carries
// nothing but their own payload.
func (i *Indexer) SetProgressWriter(w io.Writer) { i.progress = w }

// IndexProject indexes all source files in the project. Cancellation stops
// it between files and requests and returns the context error. Extraction
// builds an in-memory candidate; the published graph changes only when
// the final RefreshPlan publishes atomically (R05), so a failed or
// cancelled attempt leaves the previous snapshot intact.
func (i *Indexer) IndexProject(ctx context.Context, files []FileInfo, force bool) error {
	i.phaseDiagnostics = nil
	if err := ctx.Err(); err != nil {
		return err
	}
	cand := newCandidate()
	store := &refreshStore{published: i.db, candidate: cand}

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

		// Get LSP client for this language. Startup failures are recorded by
		// the manager; indexing continues with fallback.
		client, _ := i.lsp.GetClient(ctx, language)
		if err := ctx.Err(); err != nil {
			return err
		}
		settled := false
		var symbolFailures requestFailures

		for idx, file := range langFiles {
			if err := ctx.Err(); err != nil {
				return err
			}
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
			fmt.Fprintf(i.progress, "\r   [%s] %d/%d files (%.0f%%) ", language, idx+1, langTotal, progress)

			symbols := 0
			var err error

			if client != nil {
				// Settle only once a live server has files to index.
				if !settled {
					if err := waitForServerSettle(ctx, language); err != nil {
						return err
					}
					settled = true
				}
				symbols, err = i.indexFile(ctx, store, client, file)
				if err != nil {
					if ctxErr := ctx.Err(); ctxErr != nil {
						return ctxErr
					}
					symbolFailures.add(client, err)
				}
			} else {
				// No LSP client, force fallback
				err = fmt.Errorf("no LSP client")
			}

			// Fallback if error OR if LSP returned 0 symbols (likely failed to process)
			if err != nil || symbols == 0 {
				// Try tree-sitter fallback
				tsIndexer := NewTreeSitterIndexer(store, i.rootPath)
				tsSymbols, tsErr := tsIndexer.IndexFile(ctx, file)
				if tsErr != nil {
					if err != nil {
						fmt.Fprintf(i.progress, "\n   ⚠️  Error indexing %s: %v (tree-sitter: %v)\n", file.RelPath, err, tsErr)
					}
					// If LSP managed 0 and tree-sitter failed, we just continue (count as 0)
					continue
				}

				// Tree-sitter succeeded
				langIndexed++
				langTreeSitter++
				indexedFiles++
				totalSymbols += tsSymbols
				continue
			}

			langIndexed++
			langLSP++
			indexedFiles++
			totalSymbols += symbols
		}

		// Clear progress line and show summary with source counts
		if langIndexed > 0 {
			fmt.Fprintf(i.progress, "\r   [%s] %d indexed (%d LSP, %d tree-sitter), %d skipped         \n", language, langIndexed, langLSP, langTreeSitter, langSkipped)
		} else if langSkipped > 0 {
			fmt.Fprintf(i.progress, "\r   [%s] 0 indexed, %d skipped (unchanged)         \n", language, langSkipped)
		}
		i.recordRequestFailures("symbols", language, symbolFailures)
	}

	// Index call graph for each language
	fmt.Fprintln(i.progress, "📊 Extracting call graph (via references)...")
	callGraphIndexer := NewCallGraphIndexer(store, i.lsp, i.rootPath)
	callExtractor := NewCallExtractor(store, i.rootPath)
	totalCalls := 0
	for language := range groups {
		// Try LSP-based call graph first
		calls, err := callGraphIndexer.IndexCallGraph(ctx, language)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		i.recordRequestFailures("calls", language, callGraphIndexer.failures)
		if err != nil || calls == 0 {
			// LSP failed or returned nothing, try tree-sitter
			for _, file := range groups[language] {
				tsCount, tsErr := callExtractor.ExtractCalls(ctx, file)
				if tsErr == nil {
					totalCalls += tsCount
				}
			}
			if err != nil {
				// Only show warning if there was an actual error (not just 0 results)
				fmt.Fprintf(i.progress, "   ⚠️  Call graph LSP error for %s (using tree-sitter): %v\n", language, err)
			}
			continue
		}
		totalCalls += calls
	}
	fmt.Fprintf(i.progress, "   Found %d call relationships\n", totalCalls)

	// Index type hierarchy for each language
	fmt.Fprintln(i.progress, "🔗 Extracting type hierarchy...")
	hierarchyIndexer := NewHierarchyIndexer(store, i.lsp, i.rootPath)
	totalHierarchy := 0

	// Try LSP-based hierarchy first, fall back to tree-sitter if no results
	for language := range groups {
		count, err := hierarchyIndexer.IndexHierarchyLSP(ctx, language)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		i.recordRequestFailures("hierarchy", language, hierarchyIndexer.failures)
		if err != nil || count == 0 {
			// LSP hierarchy failed or returned nothing, try tree-sitter
			for _, file := range groups[language] {
				tsCount, tsErr := hierarchyIndexer.IndexHierarchyTreeSitter(ctx, file)
				if tsErr == nil {
					totalHierarchy += tsCount
				}
			}
			continue
		}
		totalHierarchy += count
	}
	fmt.Fprintf(i.progress, "   Found %d type relationships\n", totalHierarchy)

	// Publish the candidate: every language in the scan had its edge set
	// re-extracted this attempt, so all of them are dirty. The publication
	// is one transaction — before it, the published graph is untouched.
	if err := i.db.Publish(ctx, cand.plan(DetectedLanguages(files))); err != nil {
		return fmt.Errorf("failed to publish graph: %w", err)
	}

	// Preserve one diagnostic per language for the build boundary, then the
	// phase failures, then any server that had to be killed.
	i.diagnostics = append(i.lsp.Diagnostics(), i.phaseDiagnostics...)
	for language := range groups {
		executable := i.lsp.Executable(language)
		if err := i.lsp.ShutdownLanguage(language); err != nil {
			i.diagnostics = append(i.diagnostics, lsp.LSPDiagnostic{
				Language:   language,
				Executable: executable,
				Category:   lsp.CleanupFailed,
				Severity:   lsp.SeverityWarning,
				Reason:     err.Error(),
			})
		}
	}

	unresolved, err := i.db.GetUnresolved()
	if err != nil {
		return fmt.Errorf("failed to count unresolved relationships: %w", err)
	}
	fmt.Fprintf(i.progress, "✅ Indexed %d files, skipped %d unchanged, %d symbols, %d calls, %d type relations, %d unresolved sites\n",
		indexedFiles, skippedFiles, totalSymbols, totalCalls, totalHierarchy, len(unresolved))
	return nil
}

// Diagnostics returns the non-fatal LSP diagnostics collected during indexing.
func (i *Indexer) Diagnostics() []lsp.LSPDiagnostic {
	return append([]lsp.LSPDiagnostic(nil), i.diagnostics...)
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
func (i *Indexer) indexFile(ctx context.Context, store graphStore, client *lsp.Client, file FileInfo) (int, error) {
	// A provider attempt starts fresh for this file.
	store.BeginFile(file.Path)

	// Convert path to URI
	fileURI := lsp.PathToURI(file.Path)

	// Read file content
	content, err := os.ReadFile(file.Path)
	if err != nil {
		return 0, err
	}
	contentStr := string(content)

	// Open document in LSP
	if err := client.DidOpenTextDocument(fileURI, file.Language, contentStr); err != nil {
		return 0, fmt.Errorf("failed to open document: %w", err)
	}
	defer client.DidCloseTextDocument(fileURI)

	// Get document symbols from LSP
	symbols, err := client.DocumentSymbols(ctx, fileURI)
	if err != nil {
		return 0, err
	}

	// Store symbols in database
	lines := lsp.NewSourceLines()
	lines.Set(file.Path, contentStr)
	doc := &lspDocument{file: file, enc: client.PositionEncoding(), lines: lines, ids: idAllocator{}}
	if err := i.storeSymbols(store, doc, symbols, nil); err != nil {
		return 0, err
	}
	count := doc.count

	// Update file metadata
	if err := store.UpdateFileMeta(file.Path, time.Now(), file.Language); err != nil {
		return 0, err
	}

	return count, nil
}

// lspDocument is one file's document symbols being stored.
type lspDocument struct {
	file  FileInfo
	enc   lsp.PositionEncoding
	lines *lsp.SourceLines
	ids   idAllocator
	count int
}

// storeSymbols recursively stores symbols in the database
func (i *Indexer) storeSymbols(store graphStore, doc *lspDocument, symbols []lsp.DocumentSymbol, owners []string) error {
	file := doc.file
	for _, sym := range symbols {
		key := symbolKey{Language: file.Language, RelPath: file.RelPath, Owners: owners, Name: sym.Name}
		if file.Language == "go" {
			if receiver, method, ok := splitGoReceiver(sym.Name); ok {
				key.Owners = append(append([]string(nil), owners...), receiver)
				key.Name = method
			}
		}
		line, column := doc.lines.FromLSP(doc.enc, file.Path, sym.SelectionRange.Start)
		endLine, endColumn := doc.lines.FromLSP(doc.enc, file.Path, sym.Range.End)

		// Create database symbol
		dbSym := &db.Symbol{
			ID:            doc.ids.assign(key),
			Name:          key.Name,
			Kind:          lsp.SymbolKindToString(sym.Kind),
			File:          file.Path,
			Line:          line,
			Column:        column,
			EndLine:       intPtr(endLine),
			EndColumn:     intPtr(endColumn),
			Scope:         key.Scope(),
			Signature:     extractReturnType(sym.Detail, file.Language),
			Documentation: "",
			Language:      file.Language,
			Source:        "lsp",
			CreatedAt:     time.Now(),
		}

		if err := store.InsertSymbol(dbSym); err != nil {
			return err
		}
		doc.count++

		// Recursively process children
		if len(sym.Children) > 0 {
			childOwners := append(append([]string(nil), key.Owners...), key.Name)
			if err := i.storeSymbols(store, doc, sym.Children, childOwners); err != nil {
				return err
			}
		}
	}

	return nil
}

// Close shuts down all LSP servers and returns any cleanup failure.
func (i *Indexer) Close() error {
	return i.lsp.ShutdownAll()
}

// Helper functions

func intPtr(i int) *int {
	return &i
}

// extractReturnType extracts just the return type from a full function signature
// For Rust: "fn(&self, a: f64, b: f64) -> f64" -> "f64"
// For Java: ": double" -> "double"
func extractReturnType(signature, language string) string {
	if signature == "" {
		return ""
	}

	switch language {
	case "rust":
		// Rust format: fn(...) -> ReturnType
		if _, after, ok := strings.Cut(signature, "->"); ok {
			return strings.TrimSpace(after)
		}
		// No return type means ()
		if strings.HasPrefix(signature, "fn(") {
			return "()"
		}
	case "java":
		// Java LSP returns ": returnType" format (sometimes with leading space)
		trimmed := strings.TrimSpace(signature)
		if strings.HasPrefix(trimmed, ":") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, ":"))
		}
	}

	return signature
}
