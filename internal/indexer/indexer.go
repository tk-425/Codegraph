package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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
	attempt     *db.BuildAttempt
	snapshot    *db.Snapshot
	// phaseDiagnostics holds request failures attributed to a phase during
	// the current IndexProject call.
	phaseDiagnostics []lsp.LSPDiagnostic
	progress         io.Writer
}

// requestFailures counts the failed LSP requests of one phase and language.
// Unsupported-method replies are not failures and are not counted.
type requestFailures struct {
	executable  string
	count       int
	first       error
	unsupported int
}

func (f *requestFailures) add(client *lsp.Client, err error) {
	if lsp.IsUnsupported(err) {
		f.unsupported++
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
func (i *Indexer) IndexProject(ctx context.Context, files []FileInfo, force bool) (resultErr error) {
	i.attempt = db.NewBuildAttempt()
	i.snapshot = nil
	i.diagnostics = nil
	defer func() {
		// Setup/cancellation before durable attempt registration still gets an
		// accurate invocation-local report at the output boundary.
		if resultErr != nil && i.attempt.Outcome == "running" {
			now := time.Now().UTC()
			i.attempt.FinishedAt, i.attempt.Outcome, i.attempt.Error = &now, "failed", resultErr.Error()
			if errors.Is(resultErr, context.Canceled) || errors.Is(resultErr, context.DeadlineExceeded) {
				i.attempt.Outcome = "cancelled"
			}
		}
	}()
	i.phaseDiagnostics = nil
	if err := ctx.Err(); err != nil {
		return err
	}
	cand := newCandidate()
	store := &refreshStore{published: i.db, candidate: cand}

	// An explicit rebuild may replace a graph held in another index format;
	// the replacement itself runs inside the publication transaction, so a
	// failed rebuild leaves the legacy snapshot intact (R05, T23).
	resetFormat := false
	if force {
		if err := i.db.CheckFormat(); err != nil {
			var incompatible *db.IncompatibleFormatError
			if !errors.As(err, &incompatible) {
				return fmt.Errorf("failed to validate index format: %w", err)
			}
			resetFormat = true
		}
	}

	groups := GroupByLanguage(files)
	var expectedRevision *int64
	if !resetFormat {
		revision, err := i.db.PublicationRevision()
		if err != nil {
			return err
		}
		expectedRevision = &revision
		i.snapshot, err = i.db.Snapshot()
		if err != nil {
			return err
		}
		if err := i.db.RecordAttempt(ctx, i.attempt); err != nil {
			return err
		}
	}
	closed := false
	finishProviders := func() {
		if closed {
			return
		}
		closed = true
		i.attempt.Phases = db.SummarizePhases(i.attempt.Phases)
		i.diagnostics = append(i.lsp.Diagnostics(), i.phaseDiagnostics...)
		for language := range groups {
			executable := i.lsp.Executable(language)
			if err := i.lsp.ShutdownLanguage(language); err != nil {
				i.diagnostics = append(i.diagnostics, lsp.LSPDiagnostic{Language: language, Executable: executable, Category: lsp.CleanupFailed, Severity: lsp.SeverityWarning, Reason: err.Error()})
			}
		}
		for _, diagnostic := range i.Diagnostics() {
			i.attempt.Diagnostics = append(i.attempt.Diagnostics, db.AttemptDiagnostic{Language: diagnostic.Language, Category: string(diagnostic.Category), Severity: string(diagnostic.Severity), Reason: diagnostic.Reason})
		}
	}
	defer func() {
		finishProviders()
		if resultErr != nil {
			now := time.Now().UTC()
			i.attempt.FinishedAt, i.attempt.Outcome, i.attempt.Error = &now, "failed", resultErr.Error()
			if errors.Is(resultErr, context.Canceled) || errors.Is(resultErr, context.DeadlineExceeded) {
				i.attempt.Outcome = "cancelled"
			}
			// A cancelled command context cannot finalize its durable attempt.
			// Use an independent bounded write; never mutate an incompatible graph.
			if !resetFormat {
				// Another writer may have invalidated the starting publication while
				// this attempt ran. Do not emit that stale summary on failure.
				snapshot, snapshotErr := i.db.Snapshot()
				i.snapshot = snapshot
				if snapshotErr != nil {
					resultErr = errors.Join(resultErr, fmt.Errorf("read current publication quality: %w", snapshotErr))
				}
				finalCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := i.db.RecordAttempt(finalCtx, i.attempt); err != nil {
					resultErr = errors.Join(resultErr, fmt.Errorf("record failed attempt: %w", err))
				}
			}
		}
	}()

	// The published manifest decides what the refresh removes: files the
	// scan no longer returns (deleted, renamed, moved, newly ignored),
	// down to a language with no files left and an entirely empty tree
	// (R01).
	published, err := i.db.ListFileMeta()
	if err != nil {
		return fmt.Errorf("failed to read published manifest: %w", err)
	}
	scanSet := make(map[string]bool, len(files))
	scanLang := make(map[string]string, len(files))
	for _, file := range files {
		scanSet[file.Path] = true
		scanLang[file.Path] = file.Language
	}
	oldLang := make(map[string]string, len(published))
	for _, meta := range published {
		oldLang[meta.Path] = meta.Language
	}
	storedDigests, err := i.db.GetProviderDigests()
	if err != nil {
		return fmt.Errorf("failed to read provider state: %w", err)
	}
	var removed []string
	for _, meta := range published {
		if !scanSet[meta.Path] {
			removed = append(removed, meta.Path)
		}
	}
	sort.Strings(removed)

	// changedPaths are the files this attempt re-extracts; their old and
	// new languages end up dirty.
	changedPaths := map[string]bool{}
	// digests are this attempt's provider identity per language.
	digests := map[string]string{}
	// digestChanged marks languages whose provider identity moved.
	digestChanged := map[string]bool{}

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
		client, startupErr := i.lsp.GetClient(ctx, language)
		if startupErr != nil {
			i.attempt.Phases = append(i.attempt.Phases, db.PhaseOutcome{Language: language, Phase: "symbols", Provider: "lsp", Status: "failed", Reason: startupErr.Error()})
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		settled := false
		var symbolFailures requestFailures
		// The provider identity of this attempt: format version, resolved
		// server and live capabilities (R02).
		digest := providerDigest(language, i.lsp, client)
		digests[language] = digest
		// Missing/invalidated quality cannot be backfilled with invented historical
		// provenance. An ordinary build re-extracts to establish it, without a
		// format reset or requiring --force from an upgrading user.
		providerInvalidated := storedDigests[language] != digest || i.snapshot == nil
		digestChanged[language] = providerInvalidated
		provider := "tree-sitter"
		if client != nil {
			provider = "lsp"
		}

		for idx, file := range langFiles {
			if err := ctx.Err(); err != nil {
				return err
			}
			// The refresh decision: skip only when the fingerprint, the
			// recorded provider and the language's provider identity all
			// still hold. mtime can prove a change, never an absence of
			// one (R02).
			if !force && !providerInvalidated {
				skip, err := i.refreshDecision(file, provider)
				if err != nil {
					return err
				}
				if skip {
					langSkipped++
					skippedFiles++
					continue
				}
			}

			// Show progress
			progress := float64(idx+1) / float64(langTotal) * 100
			fmt.Fprintf(i.progress, "\r   [%s] %d/%d files (%.0f%%) ", language, idx+1, langTotal, progress)

			// Extract against stable bytes: a source mutating mid-extraction
			// is retried boundedly and never published as fresh (T05).
			var symbols int
			var source string
			var lspSucceeded bool
			stableErr := withStableSource(file.Path, stableSourceAttempts, func(content []byte, hash string, mtime time.Time) (bool, error) {
				lspSucceeded = false
				if client != nil {
					// Settle only once a live server has files to index.
					if !settled {
						if err := waitForServerSettle(ctx, language); err != nil {
							return false, err
						}
						settled = true
					}
					count, err := i.indexFile(ctx, store, client, file, content, hash, mtime)
					if err != nil {
						if ctxErr := ctx.Err(); ctxErr != nil {
							return false, ctxErr
						}
						symbolFailures.add(client, err)
					} else if count > 0 {
						symbols, source, lspSucceeded = count, "lsp", true
						return true, nil
					} else {
						i.attempt.Phases = append(i.attempt.Phases, db.PhaseOutcome{Language: language, Phase: "symbols", Provider: "lsp", Status: "complete", Files: 1, Reason: "successful empty response; syntax supplementation attempted"})
					}
				}
				tsSymbols, tsErr := NewTreeSitterIndexer(store, i.rootPath).IndexFileContent(ctx, file, content, hash, mtime)
				if tsErr != nil {
					i.attempt.Phases = append(i.attempt.Phases, db.PhaseOutcome{Language: language, Phase: "symbols", Provider: "tree-sitter", Status: "failed", Files: 1, Reason: tsErr.Error()})
					return false, fmt.Errorf("no usable symbol provider for %s: %w", file.RelPath, tsErr)
				}
				symbols, source = tsSymbols, "tree-sitter"
				return true, nil
			})
			if stableErr != nil {
				i.recordRequestFailures("symbols", language, symbolFailures)
				if symbolFailures.count > 0 || symbolFailures.unsupported > 0 {
					i.attempt.Phases = append(i.attempt.Phases, requestOutcome(language, "symbols", 0, symbolFailures, nil))
				}
				return stableErr
			}
			status := "fallback"
			if lspSucceeded {
				status = "complete"
			}
			i.attempt.Phases = append(i.attempt.Phases, db.PhaseOutcome{Language: language, Phase: "symbols", Provider: source, Status: status, Files: 1, Records: symbols})
			changedPaths[file.Path] = true
			langIndexed++
			indexedFiles++
			totalSymbols += symbols
			if source == "lsp" {
				langLSP++
			} else {
				langTreeSitter++
			}
		}

		// Clear progress line and show summary with source counts
		if langIndexed > 0 {
			fmt.Fprintf(i.progress, "\r   [%s] %d indexed (%d LSP, %d tree-sitter), %d skipped         \n", language, langIndexed, langLSP, langTreeSitter, langSkipped)
		} else if langSkipped > 0 {
			fmt.Fprintf(i.progress, "\r   [%s] 0 indexed, %d skipped (unchanged)         \n", language, langSkipped)
		}
		if langSkipped > 0 {
			i.attempt.Phases = append(i.attempt.Phases, db.PhaseOutcome{Language: language, Phase: "symbols", Provider: provider, Status: "reused", Files: langSkipped})
		}
		i.recordRequestFailures("symbols", language, symbolFailures)
		if symbolFailures.count > 0 || symbolFailures.unsupported > 0 {
			i.attempt.Phases = append(i.attempt.Phases, requestOutcome(language, "symbols", 0, symbolFailures, nil))
		}
	}

	// A language is dirty when any of its files was added, changed or
	// removed — under its old or new language — or its provider identity
	// moved; only dirty languages re-extract their edge sets, the others'
	// are already equivalent to a full rebuild's (R01, R02).
	dirty := map[string]bool{}
	for _, meta := range published {
		if !scanSet[meta.Path] {
			dirty[meta.Language] = true
		}
	}
	for path := range changedPaths {
		dirty[scanLang[path]] = true
		if old, ok := oldLang[path]; ok {
			dirty[old] = true
		}
	}
	for language, changed := range digestChanged {
		if changed {
			dirty[language] = true
		}
	}
	dirtyLanguages := make([]string, 0, len(dirty))
	for language := range dirty {
		dirtyLanguages = append(dirtyLanguages, language)
	}
	sort.Strings(dirtyLanguages)

	// Index call graph for each dirty language
	fmt.Fprintln(i.progress, "📊 Extracting call graph (via references)...")
	callGraphIndexer := NewCallGraphIndexer(store, i.lsp, i.rootPath)
	callExtractor := NewCallExtractor(store, i.rootPath)
	totalCalls := 0
	for language := range groups {
		if !dirty[language] {
			continue
		}
		// Try LSP-based call graph first
		calls, err := callGraphIndexer.IndexCallGraph(ctx, language)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		i.recordRequestFailures("calls", language, callGraphIndexer.failures)
		i.attempt.Phases = append(i.attempt.Phases, requestOutcome(language, "calls", calls, callGraphIndexer.failures, err))
		if err != nil || calls == 0 || callGraphIndexer.failures.count > 0 || callGraphIndexer.failures.unsupported > 0 {
			totalCalls += calls
			fallback := db.PhaseOutcome{Language: language, Phase: "calls", Provider: "tree-sitter", Status: "fallback"}
			// LSP failed or returned nothing, try tree-sitter
			for _, file := range groups[language] {
				if _, ok := callSyntaxes[file.Language]; !ok || grammar(file.Language) == nil {
					fallback.Status = "unsupported"
					continue
				}
				tsCount, tsErr := callExtractor.ExtractCalls(ctx, file)
				if tsErr != nil {
					fallback.Status, fallback.Reason = "failed", tsErr.Error()
					i.attempt.Phases = append(i.attempt.Phases, fallback)
					return tsErr
				}
				totalCalls += tsCount
				fallback.Records += tsCount
				fallback.Files++
			}
			i.attempt.Phases = append(i.attempt.Phases, fallback)
			if err != nil {
				// Only show warning if there was an actual error (not just 0 results)
				fmt.Fprintf(i.progress, "   ⚠️  Call graph LSP error for %s (using tree-sitter): %v\n", language, err)
			}
			continue
		}
		totalCalls += calls
	}
	fmt.Fprintf(i.progress, "   Found %d call relationships\n", totalCalls)

	// Index type hierarchy for each dirty language
	fmt.Fprintln(i.progress, "🔗 Extracting type hierarchy...")
	hierarchyIndexer := NewHierarchyIndexer(store, i.lsp, i.rootPath)
	totalHierarchy := 0

	// Try LSP-based hierarchy first, fall back to tree-sitter if no results
	for language := range groups {
		if !dirty[language] {
			continue
		}
		count, err := hierarchyIndexer.IndexHierarchyLSP(ctx, language)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		i.recordRequestFailures("hierarchy", language, hierarchyIndexer.failures)
		i.attempt.Phases = append(i.attempt.Phases, requestOutcome(language, "hierarchy", count, hierarchyIndexer.failures, err))
		if err != nil || count == 0 || hierarchyIndexer.failures.count > 0 || hierarchyIndexer.failures.unsupported > 0 {
			totalHierarchy += count
			fallback := db.PhaseOutcome{Language: language, Phase: "hierarchy", Provider: "tree-sitter", Status: "fallback"}
			// Failed/unsupported semantic extraction may use syntax fallback.
			for _, file := range groups[language] {
				if !supportsSyntaxHierarchy(file.Language) {
					fallback.Status = "unsupported"
					continue
				}
				tsCount, tsErr := hierarchyIndexer.IndexHierarchyTreeSitter(ctx, file)
				if tsErr != nil {
					fallback.Status, fallback.Reason = "failed", tsErr.Error()
					i.attempt.Phases = append(i.attempt.Phases, fallback)
					return tsErr
				}
				totalHierarchy += tsCount
				fallback.Records += tsCount
				fallback.Files++
			}
			i.attempt.Phases = append(i.attempt.Phases, fallback)
			continue
		}
		totalHierarchy += count
	}
	fmt.Fprintf(i.progress, "   Found %d type relationships\n", totalHierarchy)

	// Publish the candidate: the dirty languages' edge sets were
	// re-extracted this attempt, and the removed files disappear with it.
	// The publication is one transaction — before it, the published graph
	// is untouched.
	finishProviders()
	plan := cand.plan(removed, dirtyLanguages)
	plan.Attempt = i.attempt
	plan.ExpectedRevision = expectedRevision
	for _, phase := range i.attempt.Phases {
		if dirty[phase.Language] {
			plan.Phases = append(plan.Phases, phase)
		}
	}
	plan.ProviderState = digests
	plan.ResetFormat = resetFormat
	if err := i.db.Publish(ctx, plan); err != nil {
		return fmt.Errorf("failed to publish graph: %w", err)
	}

	i.snapshot = plan.PublishedSnapshot
	unresolved := 0
	for _, count := range i.snapshot.Counts.Unresolved {
		unresolved += count
	}
	fmt.Fprintf(i.progress, "✅ Indexed %d files, skipped %d unchanged, %d symbols, %d calls, %d type relations, %d unresolved sites\n",
		indexedFiles, skippedFiles, totalSymbols, totalCalls, totalHierarchy, unresolved)
	return nil
}

// Diagnostics returns the non-fatal LSP diagnostics collected during indexing.
func (i *Indexer) Diagnostics() []lsp.LSPDiagnostic {
	return append([]lsp.LSPDiagnostic(nil), i.diagnostics...)
}

// Attempt and Snapshot expose this invocation's report without rereading a
// possibly newer concurrent publication at the CLI boundary.
func (i *Indexer) Attempt() *db.BuildAttempt { return i.attempt }
func (i *Indexer) Snapshot() *db.Snapshot    { return i.snapshot }

func requestOutcome(language, phase string, records int, failures requestFailures, err error) db.PhaseOutcome {
	outcome := db.PhaseOutcome{Language: language, Phase: phase, Provider: "lsp", Status: "complete", Records: records, FailedRequests: failures.count}
	if failures.unsupported > 0 {
		outcome.Status = "unsupported"
		outcome.Reason = "provider does not support required requests"
	}
	if err != nil || failures.count > 0 {
		outcome.Status = "failed"
		if err != nil {
			outcome.Reason = err.Error()
		} else if failures.first != nil {
			outcome.Reason = failures.first.Error()
		}
	}
	return outcome
}

func supportsSyntaxHierarchy(language string) bool {
	if grammar(language) == nil {
		return false
	}
	switch language {
	case "csharp", "java", "typescript", "typescriptreact", "javascript", "python", "swift", "rust", "go", "ocaml":
		return true
	}
	return false
}

// stableSourceAttempts bounds how often a mutating source is retried
// during one file's extraction before the attempt fails (T05).
const stableSourceAttempts = 3

// fingerprint is the content identity a refresh compares: the hex SHA-256
// of the indexed bytes (R02).
func fingerprint(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// providerDigest fingerprints what extraction under language depends on:
// the index format's identity version, the resolved server command and
// arguments (recorded even when its startup failed), and — when a server
// is live — the capabilities that shape extraction (R02).
func providerDigest(language string, mgr *lsp.Manager, client *lsp.Client) string {
	h := sha256.New()
	fmt.Fprintf(h, "format=%d", db.FormatVersion)
	if command, args, ok := mgr.Server(language); ok {
		fmt.Fprintf(h, ";command=%s;args=%v", command, args)
	} else {
		fmt.Fprintf(h, ";command=<none>")
	}
	if client != nil {
		caps := client.Capabilities()
		fmt.Fprintf(h, ";callHierarchy=%t;typeHierarchy=%t",
			lsp.Supports(caps.CallHierarchyProvider), lsp.Supports(caps.TypeHierarchyProvider))
	} else {
		fmt.Fprintf(h, ";provider=tree-sitter")
	}
	return hex.EncodeToString(h.Sum(nil))
}

// refreshDecision reports whether the refresh may skip file under the
// provider that would extract it now. A file is skippable only when the
// recorded fingerprint and provider still hold: mtime can shortcut a
// change, never an absence of one (R02).
func (i *Indexer) refreshDecision(file FileInfo, provider string) (bool, error) {
	stat, err := os.Stat(file.Path)
	if err != nil {
		return false, err
	}
	meta, err := i.db.GetFileMeta(file.Path)
	if err != nil {
		return false, err
	}
	if meta == nil || meta.Provider != provider {
		return false, nil
	}
	if stat.ModTime().After(meta.ModTime) {
		return false, nil
	}
	content, err := os.ReadFile(file.Path)
	if err != nil {
		return false, err
	}
	return fingerprint(content) == meta.ContentHash, nil
}

// withStableSource runs extract against the current bytes of path and
// verifies the source did not change while it ran. A mutating source is
// retried with the fresh bytes, bounded by attempts; past the bound the
// error names the file — inconsistent analyzed bytes are never published
// as a fresh snapshot (T05).
func withStableSource(path string, attempts int, extract func(content []byte, hash string, mtime time.Time) (bool, error)) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	hash := fingerprint(content)
	mtime := sourceModTime(path)
	for attempt := 1; ; attempt++ {
		extracted, err := extract(content, hash, mtime)
		if err != nil {
			return err
		}
		if !extracted {
			return nil
		}
		after, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		afterHash := fingerprint(after)
		if afterHash == hash {
			return nil
		}
		if attempt >= attempts {
			return fmt.Errorf("source of %s changed during extraction; not published as fresh", path)
		}
		content, hash, mtime = after, afterHash, sourceModTime(path)
	}
}

// sourceModTime is the mtime recorded for path, or the zero time when it
// cannot be stated.
func sourceModTime(path string) time.Time {
	if stat, err := os.Stat(path); err == nil {
		return stat.ModTime()
	}
	return time.Time{}
}

// indexFile indexes a single file and returns number of symbols stored
func (i *Indexer) indexFile(ctx context.Context, store graphStore, client *lsp.Client, file FileInfo, content []byte, hash string, mtime time.Time) (int, error) {
	// A provider attempt starts fresh for this file.
	store.BeginFile(file.Path)

	// Convert path to URI
	fileURI := lsp.PathToURI(file.Path)
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

	// Record the refresh metadata: fingerprint, provider, source mtime and
	// extraction time (R02).
	if err := store.RecordFileMeta(db.FileMeta{Path: file.Path, ModTime: mtime, Language: file.Language, ContentHash: hash, Provider: "lsp", ExtractedAt: time.Now()}); err != nil {
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
