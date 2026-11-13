package indexer

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tk-425/Codegraph/internal/config"
	"github.com/tk-425/Codegraph/internal/db"
)

// refreshProject is an ordinary (untagged) harness for refresh behaviour:
// real SQLite, real scanner, LSP servers pointed at a path that cannot
// exist, so extraction always runs the tree-sitter fallback.
type refreshProject struct {
	root     string
	database *db.Manager
	cfg      *config.Config
}

func newRefreshProject(t *testing.T) *refreshProject {
	t.Helper()
	root := t.TempDir()
	database, err := db.NewManager(filepath.Join(root, ".codegraph", "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := database.Initialize(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".cgignore"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	for language := range cfg.LSP {
		cfg.LSP[language] = config.LSPConfig{Command: filepath.Join(root, "unavailable-lsp")}
	}
	return &refreshProject{root: root, database: database, cfg: cfg}
}

func (p *refreshProject) write(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(p.root, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (p *refreshProject) build(t *testing.T, force bool) error {
	t.Helper()
	scanner, err := NewScanner(p.root, filepath.Join(p.root, ".cgignore"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	indexer := NewIndexer(p.cfg, p.database, p.root)
	indexer.SetProgressWriter(io.Discard)
	defer indexer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return indexer.IndexProject(ctx, files, force)
}

func (p *refreshProject) symbols(t *testing.T, name string) []db.Symbol {
	t.Helper()
	symbols, err := p.database.GetSymbolByName(name, nil)
	if err != nil {
		t.Fatal(err)
	}
	return symbols
}

const goCallerCallee = `package main

func Target() {}

func Caller() { Target() }
`

const goOther = `package main

func Other() {}
`

const pythonWorker = `class Worker:
    def run(self):
        pass

def main():
    w = Worker()
    w.run()
`

// T02: deleting a file removes its symbols, its metadata and the edges that
// reached it; the rest of the graph survives.
func TestRefreshRemovesDeletedFile(t *testing.T) {
	p := newRefreshProject(t)
	p.write(t, "source.go", goCallerCallee)
	p.write(t, "other.go", goOther)
	if err := p.build(t, true); err != nil {
		t.Fatal(err)
	}
	if symbols := p.symbols(t, "Target"); len(symbols) != 1 {
		t.Fatalf("fixture did not publish Target: %+v", symbols)
	}

	if err := os.Remove(filepath.Join(p.root, "source.go")); err != nil {
		t.Fatal(err)
	}
	if err := p.build(t, false); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"Target", "Caller"} {
		if symbols := p.symbols(t, name); len(symbols) != 0 {
			t.Errorf("T02: deleted file's %s survived: %+v", name, symbols)
		}
	}
	if symbols := p.symbols(t, "Other"); len(symbols) != 1 {
		t.Errorf("T02: undeleted file's symbol lost: %+v", symbols)
	}
	if callers, err := p.database.GetCallers("Target", nil); err != nil || len(callers) != 0 {
		t.Errorf("T02: edges of deleted file survived: %+v, %v", callers, err)
	}
	if meta, err := p.database.GetFileMeta(filepath.Join(p.root, "source.go")); err != nil || meta != nil {
		t.Errorf("T02: deleted file's metadata survived: %+v, %v", meta, err)
	}
}

// T02: renaming or moving a file is a remove plus an add for the refresh.
func TestRefreshMovesFile(t *testing.T) {
	p := newRefreshProject(t)
	p.write(t, "source.go", goCallerCallee)
	if err := p.build(t, true); err != nil {
		t.Fatal(err)
	}

	if err := os.Rename(filepath.Join(p.root, "source.go"), filepath.Join(p.root, "moved.go")); err != nil {
		t.Fatal(err)
	}
	if err := p.build(t, false); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"Target", "Caller"} {
		symbols := p.symbols(t, name)
		if len(symbols) != 1 || symbols[0].File != filepath.Join(p.root, "moved.go") {
			t.Errorf("T02: %s not moved: %+v", name, symbols)
		}
	}
	stats, err := p.database.GetStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.SymbolCount != 2 || stats.CallCount != 1 {
		t.Errorf("T02: moved file left duplicates: %d symbols, %d calls", stats.SymbolCount, stats.CallCount)
	}
}

// T04: a language that loses its last file disappears entirely while other
// languages survive; an entirely empty tree leaves a coherent empty
// snapshot, not stale success.
func TestRefreshRemovesLanguageAndEmptyTree(t *testing.T) {
	p := newRefreshProject(t)
	p.write(t, "source.go", goCallerCallee)
	p.write(t, "worker.py", pythonWorker)
	if err := p.build(t, true); err != nil {
		t.Fatal(err)
	}
	if symbols := p.symbols(t, "Target"); len(symbols) != 1 {
		t.Fatalf("fixture did not publish go symbols: %+v", symbols)
	}
	if symbols := p.symbols(t, "run"); len(symbols) != 1 {
		t.Fatalf("fixture did not publish python symbols: %+v", symbols)
	}

	if err := os.Remove(filepath.Join(p.root, "worker.py")); err != nil {
		t.Fatal(err)
	}
	if err := p.build(t, false); err != nil {
		t.Fatal(err)
	}
	if symbols := p.symbols(t, "run"); len(symbols) != 0 {
		t.Errorf("T04: last python file's symbols survived: %+v", symbols)
	}
	if symbols := p.symbols(t, "Target"); len(symbols) != 1 {
		t.Errorf("T04: go symbols lost with python removal: %+v", symbols)
	}

	if err := os.WriteFile(filepath.Join(p.root, ".cgignore"), []byte("*\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := p.build(t, false); err != nil {
		t.Fatal(err)
	}
	stats, err := p.database.GetStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.SymbolCount != 0 || stats.CallCount != 0 {
		t.Errorf("T04: empty tree left %d symbols, %d calls; want 0, 0", stats.SymbolCount, stats.CallCount)
	}
	if manifest, err := p.database.ListFileMeta(); err != nil || len(manifest) != 0 {
		t.Errorf("T04: empty tree left manifest rows: %+v, %v", manifest, err)
	}
	if unresolved, err := p.database.GetUnresolved(); err != nil || len(unresolved) != 0 {
		t.Errorf("T04: empty tree left unresolved rows: %+v, %v", unresolved, err)
	}
}

const goWithOrphan = `package main

func Target() {}

func Caller() { Target() }

func Orphan() {}
`

// T07: provider configuration changes re-extract unchanged bytes. Extraction
// timestamps are the oracle: out-of-band row tampering now independently
// invalidates quality, so it cannot serve as a provider-only control.
func TestProviderConfigChangeReextracts(t *testing.T) {
	p := newRefreshProject(t)
	p.write(t, "source.go", goWithOrphan)
	if err := p.build(t, true); err != nil {
		t.Fatal(err)
	}
	orphan := p.symbols(t, "Orphan")
	if len(orphan) != 1 {
		t.Fatalf("fixture did not publish Orphan: %+v", orphan)
	}

	before, err := p.database.GetFileMeta(orphan[0].File)
	if err != nil || before == nil {
		t.Fatalf("initial metadata: %+v, %v", before, err)
	}
	// Unchanged provider: the file's extraction time stays unchanged.
	if err := p.build(t, false); err != nil {
		t.Fatal(err)
	}
	unchanged, err := p.database.GetFileMeta(orphan[0].File)
	if err != nil || unchanged == nil || !unchanged.ExtractedAt.Equal(before.ExtractedAt) {
		t.Fatalf("control: unchanged provider re-extracted: before=%+v after=%+v error=%v", before, unchanged, err)
	}

	// A different effective server command changes the provider digest.
	p.cfg.LSP["go"] = config.LSPConfig{Command: filepath.Join(p.root, "another-unavailable-lsp")}
	if err := p.build(t, false); err != nil {
		t.Fatal(err)
	}
	changed, err := p.database.GetFileMeta(orphan[0].File)
	if err != nil || changed == nil || !changed.ExtractedAt.After(before.ExtractedAt) {
		t.Errorf("T07: changed provider did not re-extract: before=%+v after=%+v error=%v", before, changed, err)
	}
	if symbols := p.symbols(t, "Orphan"); len(symbols) != 1 {
		t.Errorf("T07: re-extraction lost Orphan: %+v", symbols)
	}
}

// R02: a valid prior-generation snapshot must refresh on an extractor change,
// even when its source bytes and effective server configuration are unchanged.
func TestExtractorRevisionRefreshesUnchangedSource(t *testing.T) {
	p := newRefreshProject(t)
	p.write(t, "source.go", "package fixture\nfunc Target(\n value int,\n) int { return value }\n")
	if err := p.build(t, false); err != nil {
		t.Fatal(err)
	}
	symbols := p.symbols(t, "Target")
	if len(symbols) != 1 {
		t.Fatalf("fixture needs Target: %+v", symbols)
	}
	meta, err := p.database.GetFileMeta(symbols[0].File)
	if err != nil || meta == nil {
		t.Fatalf("fixture file metadata=%+v error=%v", meta, err)
	}
	// Seed the published input contract from before extractor revisions existed.
	server := p.cfg.LSP["go"]
	legacyInput := fmt.Sprintf("format=%d;command=%s;args=%v;provider=tree-sitter", db.FormatVersion, server.Command, server.Args)
	legacyDigest := fmt.Sprintf("%x", sha256.Sum256([]byte(legacyInput)))
	symbols[0].Signature = "func Target("
	meta.ExtractedAt = time.Unix(1, 0)
	plan := &db.RefreshPlan{
		ChangedFiles: []string{meta.Path}, DirtyLanguages: []string{"go"},
		Symbols: []*db.Symbol{&symbols[0]}, FileMeta: []db.FileMeta{*meta},
		ProviderState: map[string]string{"go": legacyDigest},
	}
	if err := p.database.Publish(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	legacy, err := p.database.Snapshot()
	if err != nil || legacy == nil {
		t.Fatalf("prior-generation snapshot must be valid, not merely invalidated: %+v %v", legacy, err)
	}
	if err := p.build(t, false); err != nil {
		t.Fatal(err)
	}
	refreshed := p.symbols(t, "Target")
	if len(refreshed) != 1 || refreshed[0].Signature != "func Target(\n value int,\n) int" {
		t.Fatalf("unchanged source retained prior extraction: %+v", refreshed)
	}
	afterMeta, err := p.database.GetFileMeta(meta.Path)
	if err != nil || afterMeta == nil || !afterMeta.ExtractedAt.After(meta.ExtractedAt) || afterMeta.ContentHash != meta.ContentHash || !afterMeta.ModTime.Equal(meta.ModTime) {
		t.Fatalf("extractor refresh changed inputs or skipped extraction: before=%+v after=%+v error=%v", meta, afterMeta, err)
	}
	digests, err := p.database.GetProviderDigests()
	if err != nil || digests["go"] == legacyDigest || digests["go"] == "" {
		t.Fatalf("extractor revision not recorded: %v %v", digests, err)
	}
	snapshot, err := p.database.Snapshot()
	if err != nil || snapshot == nil || snapshot.ID == legacy.ID {
		t.Fatalf("refresh did not publish: %+v %v", snapshot, err)
	}
	if err := p.build(t, false); err != nil {
		t.Fatal(err)
	}
	unchanged, err := p.database.GetFileMeta(meta.Path)
	if err != nil || unchanged == nil || !unchanged.ExtractedAt.Equal(afterMeta.ExtractedAt) {
		t.Fatalf("same revision re-extracted: %+v %v", unchanged, err)
	}
	noOp, err := p.database.Snapshot()
	if err != nil || noOp == nil || noOp.ID != snapshot.ID || !noOp.PublishedAt.Equal(snapshot.PublishedAt) {
		t.Fatalf("no-op replaced publication: %+v %v", noOp, err)
	}
}

// T08: a server that has become available upgrades unchanged files stuck
// on fallback data instead of skipping them forever.
func TestLSPRestoredUpgradesFallbackSymbols(t *testing.T) {
	p := newRefreshProject(t)
	p.write(t, "source.go", goCallerCallee)
	if err := p.build(t, true); err != nil {
		t.Fatal(err)
	}
	if symbols := p.symbols(t, "Target"); len(symbols) != 1 || symbols[0].Source != "tree-sitter" {
		t.Fatalf("fixture did not extract with fallback: %+v", symbols)
	}

	useScriptedServer(t, p.cfg, "go", "mixed-symbols.json")
	if err := p.build(t, false); err != nil {
		t.Fatal(err)
	}
	symbols := p.symbols(t, "Target")
	if len(symbols) != 1 || symbols[0].Source != "lsp" {
		t.Errorf("T08: fallback data not upgraded to the live provider: %+v", symbols)
	}
	meta, err := p.database.GetFileMeta(filepath.Join(p.root, "source.go"))
	if err != nil || meta == nil || meta.Provider != "lsp" {
		t.Errorf("T08: recorded provider not upgraded: %+v, %v", meta, err)
	}
}

// T05: a source that mutates while it is being extracted is retried with
// fresh bytes, and a source that never stops mutating fails the attempt
// naming the file instead of publishing mixed bytes.
func TestWithStableSourceRetriesAndFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "moving.go")
	if err := os.WriteFile(path, []byte("// v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	mutations := 1 // the first extraction rewrites the file under itself
	var seen []string
	if err := withStableSource(path, 3, func(content []byte, hash string, mtime time.Time) (bool, error) {
		seen = append(seen, hash)
		if mutations > 0 {
			mutations--
			if werr := os.WriteFile(path, []byte("// v2\n"), 0o644); werr != nil {
				return false, werr
			}
		}
		return true, nil
	}); err != nil {
		t.Fatalf("stable retry failed: %v", err)
	}
	if len(seen) != 2 || seen[0] == seen[1] {
		t.Errorf("T05: retry did not use fresh bytes: %d attempts %v", len(seen), seen)
	}

	if err := os.WriteFile(path, []byte("// base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := withStableSource(path, 3, func(content []byte, hash string, mtime time.Time) (bool, error) {
		if werr := os.WriteFile(path, append(append([]byte{}, content...), '-'), 0o644); werr != nil {
			return false, werr
		}
		return true, nil
	})
	if err == nil || !strings.Contains(err.Error(), "moving.go") {
		t.Fatalf("T05: never-stable source did not fail naming the file: %v", err)
	}
}
