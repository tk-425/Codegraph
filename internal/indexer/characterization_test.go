//go:build characterization

package indexer

import (
	"context"
	"database/sql"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/tk-425/Codegraph/internal/config"
	"github.com/tk-425/Codegraph/internal/db"
	"github.com/tk-425/Codegraph/internal/lsp"
)

// These opt-in checks assert the desired contracts. Failures characterize
// known defects; a successful ordinary test run does not execute these checks.
type characterizationProject struct {
	root     string
	dbPath   string
	database *db.Manager
	cfg      *config.Config
}

func newCharacterizationProject(t *testing.T) *characterizationProject {
	t.Helper()
	root := t.TempDir()
	dbPath := filepath.Join(root, ".codegraph", "graph.db")
	database, err := db.NewManager(dbPath)
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
	return &characterizationProject{root, dbPath, database, cfg}
}

func (p *characterizationProject) fixture(t *testing.T, source, name, language string) FileInfo {
	t.Helper()
	content, err := os.ReadFile(filepath.Join("..", "..", "testdata", "characterization", source))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(p.root, name)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return FileInfo{Path: path, RelPath: name, Language: language}
}

func (p *characterizationProject) build(t *testing.T, force bool) error {
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return indexer.IndexProject(ctx, files, force)
}

func (p *characterizationProject) symbols(t *testing.T, name string) []db.Symbol {
	t.Helper()
	symbols, err := p.database.GetSymbolByName(name, nil)
	if err != nil {
		t.Fatal(err)
	}
	return symbols
}

func (p *characterizationProject) syntax(t *testing.T, file FileInfo) {
	t.Helper()
	if _, err := NewTreeSitterIndexer(p.database, p.root).IndexFile(context.Background(), file); err != nil {
		t.Fatal(err)
	}
}

func TestCharacterizationControlFallback(t *testing.T) {
	p := newCharacterizationProject(t)
	p.fixture(t, "refresh/before.go", "source.go", "go")
	if err := p.build(t, true); err != nil {
		t.Fatal(err)
	}
	symbols := p.symbols(t, "Target")
	if len(symbols) != 1 || symbols[0].Source != "tree-sitter" {
		t.Fatalf("fallback symbols = %+v, want one syntax-extracted Target", symbols)
	}
	callers, err := p.database.GetCallers("Target", nil)
	if err != nil || len(callers) != 1 || callers[0].Name != "Caller" {
		t.Fatalf("fallback callers = %+v, err=%v; want Caller", callers, err)
	}
}

func TestCharacterizationF01ChangedFileReplacesSymbols(t *testing.T) {
	p := newCharacterizationProject(t)
	file := p.fixture(t, "refresh/before.go", "source.go", "go")
	if err := p.build(t, true); err != nil {
		t.Fatal(err)
	}
	meta, err := p.database.GetFileMeta(file.Path)
	if err != nil || meta == nil {
		t.Fatalf("initial metadata: %+v, %v", meta, err)
	}
	p.fixture(t, "refresh/after.go", "source.go", "go")
	// Force the existing mtime gate open without a scheduling-dependent sleep.
	changed := meta.ModTime.Add(time.Second)
	if err := os.Chtimes(file.Path, changed, changed); err != nil {
		t.Fatal(err)
	}
	if err := p.build(t, false); err != nil {
		t.Fatal(err)
	}
	if symbols := p.symbols(t, "Replacement"); len(symbols) != 1 {
		t.Fatalf("replacement symbol count = %d, want 1", len(symbols))
	}
	if symbols := p.symbols(t, "Target"); len(symbols) != 0 {
		t.Errorf("T01: obsolete Target survives refresh: %+v", symbols)
	}
}

func TestCharacterizationF01PreservedMtimeDetectsBytes(t *testing.T) {
	p := newCharacterizationProject(t)
	file := p.fixture(t, "refresh/before.go", "source.go", "go")
	stat, err := os.Stat(file.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.build(t, true); err != nil {
		t.Fatal(err)
	}
	p.fixture(t, "refresh/after.go", "source.go", "go")
	if err := os.Chtimes(file.Path, stat.ModTime(), stat.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := p.build(t, false); err != nil {
		t.Fatal(err)
	}
	if symbols := p.symbols(t, "Replacement"); len(symbols) != 1 {
		t.Errorf("T06: changed bytes with preserved mtime were skipped; Replacement count=%d", len(symbols))
	}
}

func TestCharacterizationF01NewIgnoreReconcilesEmptyTree(t *testing.T) {
	for _, force := range []bool{false, true} {
		name := "incremental"
		if force {
			name = "force"
		}
		t.Run(name, func(t *testing.T) {
			p := newCharacterizationProject(t)
			p.fixture(t, "refresh/before.go", "source.go", "go")
			if err := p.build(t, true); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(p.root, ".cgignore"), []byte("*.go\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := p.build(t, force); err != nil {
				t.Fatal(err)
			}
			if symbols := p.symbols(t, "Target"); len(symbols) != 0 {
				t.Errorf("T03/T04: ignored last source file remains indexed: %+v", symbols)
			}
		})
	}
}

func TestCharacterizationF02GoReceiversHaveDistinctIDs(t *testing.T) {
	p := newCharacterizationProject(t)
	file := p.fixture(t, "identity/receivers.go", "receivers.go", "go")
	p.syntax(t, file)
	if symbols := p.symbols(t, "Run"); len(symbols) != 2 {
		t.Errorf("T09: receiver-specific Run declarations collapsed: %+v", symbols)
	}
}

func TestCharacterizationF02PythonCallerUsesIndexedID(t *testing.T) {
	p := newCharacterizationProject(t)
	file := p.fixture(t, "identity/scopes.py", "scopes.py", "python")
	p.syntax(t, file)
	if symbols := p.symbols(t, "run"); len(symbols) != 1 || symbols[0].Scope != "Worker" {
		t.Fatalf("fixture did not index Worker.run: %+v", symbols)
	}
	if _, err := NewCallExtractor(p.database, p.root).ExtractCalls(context.Background(), file); err != nil {
		t.Fatal(err)
	}
	callers, err := p.database.GetCallers("target", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(callers) != 1 || callers[0].Scope != "Worker" {
		t.Errorf("T11: Worker.run invocation has no valid indexed caller: %+v", callers)
	}
}

func characterizationPeerManager(t *testing.T, p *characterizationProject, language, responses string) *lsp.Manager {
	t.Helper()
	peer := filepath.Join(t.TempDir(), "scripted-lsp")
	fixtureRoot, err := filepath.Abs(filepath.Join("..", "..", "testdata", "characterization", "lsp"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "build", "-o", peer, fixtureRoot)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build protocol peer: %v\n%s", err, output)
	}
	p.cfg.LSP[language] = config.LSPConfig{
		Command: peer,
		Args:    []string{"-responses", filepath.Join(fixtureRoot, responses)},
	}
	rootURI := (&url.URL{Scheme: "file", Path: p.root}).String()
	manager := lsp.NewManager(p.cfg, rootURI)
	t.Cleanup(func() { _ = manager.ShutdownAll() })
	return manager
}

func TestCharacterizationControlProtocolFixtures(t *testing.T) {
	for _, test := range []struct {
		name      string
		language  string
		responses string
	}{
		{"references", "go", "references.json"},
		{"hierarchy", "python", "hierarchy.json"},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := newCharacterizationProject(t)
			manager := characterizationPeerManager(t, p, test.language, test.responses)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			client, err := manager.GetClient(ctx, test.language)
			if err != nil {
				t.Fatal(err)
			}
			if test.name == "references" {
				uri := (&url.URL{Scheme: "file", Path: filepath.Join(p.root, "references.go")}).String()
				refs, err := client.References(ctx, uri, lsp.Position{Line: 2}, false)
				if err != nil || len(refs) != 1 || refs[0].URI != uri || refs[0].Range.Start.Line != 5 {
					t.Fatalf("reference fixture response=%+v, err=%v", refs, err)
				}
				return
			}
			uri := (&url.URL{Scheme: "file", Path: filepath.Join(p.root, "parents.py")}).String()
			items, err := client.PrepareTypeHierarchy(ctx, uri, lsp.Position{Line: 3})
			if err != nil || len(items) != 1 || items[0].Name != "Derived" {
				t.Fatalf("hierarchy fixture response=%+v, err=%v", items, err)
			}
			parents, err := client.Supertypes(ctx, items[0])
			if err != nil || len(parents) != 1 || parents[0].Name != "Base" || parents[0].URI != uri {
				t.Fatalf("parent fixture response=%+v, err=%v", parents, err)
			}
		})
	}
}

func TestCharacterizationF03ReferenceIsNotInvocation(t *testing.T) {
	p := newCharacterizationProject(t)
	file := p.fixture(t, "calls/references.go", "references.go", "go")
	p.syntax(t, file)
	manager := characterizationPeerManager(t, p, "go", "references.json")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := NewCallGraphIndexer(p.database, manager, p.root).IndexCallGraph(ctx, "go"); err != nil {
		t.Fatal(err)
	}
	callers, err := p.database.GetCallers("Target", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(callers) != 0 {
		t.Errorf("T14: function value reference was stored as a call: %+v", callers)
	}
}

func TestCharacterizationF04HierarchyResolvesLocalParent(t *testing.T) {
	p := newCharacterizationProject(t)
	file := p.fixture(t, "hierarchy/parents.py", "parents.py", "python")
	p.syntax(t, file)
	parents := p.symbols(t, "Base")
	if len(parents) != 1 {
		t.Fatalf("Base fixture symbols = %+v", parents)
	}
	manager := characterizationPeerManager(t, p, "python", "hierarchy.json")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := NewHierarchyIndexer(p.database, manager, p.root).IndexHierarchyLSP(ctx, "python"); err != nil {
		t.Fatal(err)
	}
	children, err := p.database.GetImplementations(parents[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 1 || children[0].Name != "Derived" {
		t.Errorf("T16: semantic parent did not resolve to indexed Base ID: %+v", children)
	}
}

func TestCharacterizationF05RepeatedFallbackIsIdempotent(t *testing.T) {
	p := newCharacterizationProject(t)
	p.fixture(t, "refresh/before.go", "source.go", "go")
	if err := p.build(t, true); err != nil {
		t.Fatal(err)
	}
	before, err := p.database.GetStats()
	if err != nil || before.CallCount != 1 {
		t.Fatalf("initial fallback stats=%+v, err=%v; want one edge", before, err)
	}
	if err := p.build(t, false); err != nil {
		t.Fatal(err)
	}
	after, err := p.database.GetStats()
	if err != nil {
		t.Fatal(err)
	}
	if after.CallCount != before.CallCount {
		t.Errorf("T19: repeated fallback inflated calls from %d to %d", before.CallCount, after.CallCount)
	}
}

func TestCharacterizationF05FailedForcePreservesPublishedGraph(t *testing.T) {
	p := newCharacterizationProject(t)
	p.fixture(t, "refresh/before.go", "source.go", "go")
	if err := p.build(t, true); err != nil {
		t.Fatal(err)
	}
	connection, err := sql.Open("sqlite3", p.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	// A real SQLite write failure, not a mocked manager or injected result.
	_, err = connection.Exec(`CREATE TRIGGER reject_replacement BEFORE INSERT ON symbols
		WHEN NEW.name = 'Replacement' BEGIN SELECT RAISE(ABORT, 'S0 write failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	p.fixture(t, "refresh/after.go", "source.go", "go")
	if err := p.build(t, true); err == nil {
		t.Error("T20: forced build swallowed the database write failure")
	}
	if symbols := p.symbols(t, "Target"); len(symbols) != 1 {
		t.Errorf("T20: failed replacement destroyed the previous Target: %+v", symbols)
	}
}
