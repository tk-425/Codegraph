package indexer

import (
	"context"
	"io"
	"os"
	"path/filepath"
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
