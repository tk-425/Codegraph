package indexer

import (
	"context"
	"encoding/json"
	"github.com/tk-425/Codegraph/internal/config"
	"github.com/tk-425/Codegraph/internal/lsp"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompleteSemanticPublication(t *testing.T) {
	p := newTransportProject(t, "hierarchy/parents.py", "source.py")
	content := "class Base:\n    pass\n\nclass Derived(Base):\n    pass\n\ndef target():\n    pass\n\ndef caller():\n    target()\n"
	if err := os.WriteFile(filepath.Join(p.root, "source.py"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(peerFixture("hierarchy.json"))
	if err != nil {
		t.Fatal(err)
	}
	responses := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(strings.ReplaceAll(string(data), "parents.py", "source.py")), &responses); err != nil {
		t.Fatal(err)
	}
	var symbols []lsp.DocumentSymbol
	for _, declaration := range []struct {
		name              string
		line, end, column int
		kind              lsp.SymbolKind
	}{
		{"Base", 0, 1, 6, lsp.SymbolKindClass}, {"Derived", 3, 4, 6, lsp.SymbolKindClass},
		{"target", 6, 7, 4, lsp.SymbolKindFunction}, {"caller", 9, 10, 4, lsp.SymbolKindFunction},
	} {
		symbols = append(symbols, lsp.DocumentSymbol{Name: declaration.name, Kind: declaration.kind,
			Range:          lsp.Range{Start: lsp.Position{Line: declaration.line}, End: lsp.Position{Line: declaration.end, Character: 16}},
			SelectionRange: lsp.Range{Start: lsp.Position{Line: declaration.line, Character: declaration.column}, End: lsp.Position{Line: declaration.line, Character: declaration.column + len(declaration.name)}}})
	}
	responses["textDocument/documentSymbol"], err = json.Marshal(symbols)
	if err != nil {
		t.Fatal(err)
	}
	responses["textDocument/references@6"] = json.RawMessage(`[{"uri":"${ROOT_URI}/source.py","range":{"start":{"line":10,"character":4},"end":{"line":10,"character":10}}}]`)
	fixture := filepath.Join(t.TempDir(), "complete.json")
	data, err = json.Marshal(responses)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture, data, 0o644); err != nil {
		t.Fatal(err)
	}
	p.cfg.LSP["python"] = config.LSPConfig{Command: scriptedPeer(t), Args: []string{"-responses", fixture}}
	if _, err := p.index(context.Background(), t); err != nil {
		t.Fatal(err)
	}
	snapshot, err := p.database.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Quality != "complete" || snapshot.Counts.Calls != 1 || snapshot.Counts.TypeRelationships != 1 || snapshot.Counts.Symbols != 4 {
		t.Fatalf("semantic snapshot=%+v", snapshot)
	}
	callees, err := p.database.GetCallees("caller", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(callees) != 1 || callees[0].Evidence != "lsp-validated-reference" {
		t.Fatalf("semantic evidence=%+v", callees)
	}
}

func TestSyntaxExtractionUsesSuppliedBytes(t *testing.T) {
	p := newRefreshProject(t)
	p.write(t, "source.go", "package fixture\nfunc DiskVersion() {}\n")
	content := []byte("package fixture\nfunc AnalyzedVersion() {}\n")
	file := FileInfo{Path: filepath.Join(p.root, "source.go"), RelPath: "source.go", Language: "go"}
	if _, err := NewTreeSitterIndexer(p.database, p.root).IndexFileContent(context.Background(), file, content, fingerprint(content), time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := p.symbols(t, "AnalyzedVersion"); len(got) != 1 {
		t.Fatalf("did not index supplied analyzed bytes: %+v", got)
	}
	if got := p.symbols(t, "DiskVersion"); len(got) != 0 {
		t.Fatalf("silently re-read disk bytes: %+v", got)
	}
}

func TestQualityFallbackUnsupportedAndNoOp(t *testing.T) {
	p := newRefreshProject(t)
	p.write(t, "source.c", "int target(void) { return 1; }\n")
	if err := p.build(t, false); err != nil {
		t.Fatal(err)
	}
	first, err := p.database.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if first.Quality != "degraded" || first.Counts.Files != 1 || first.SourceDigest == "" || first.ProviderDigest == "" {
		t.Fatalf("snapshot=%+v", first)
	}
	for _, phase := range []string{"calls", "hierarchy"} {
		found := false
		for _, outcome := range first.Phases {
			if outcome.Phase == phase && outcome.Provider == "tree-sitter" && outcome.Status == "unsupported" {
				found = true
			}
		}
		if !found {
			t.Errorf("unsupported %s not recorded: %+v", phase, first.Phases)
		}
	}
	if err := p.build(t, false); err != nil {
		t.Fatal(err)
	}
	after, err := p.database.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := p.database.LatestAttempt()
	if err != nil {
		t.Fatal(err)
	}
	if after.ID != first.ID || !after.PublishedAt.Equal(first.PublishedAt) || after.Quality != first.Quality || attempt.Changed || attempt.Outcome != "succeeded" {
		t.Fatalf("no-op snapshot=%+v attempt=%+v", after, attempt)
	}
}

func TestSuccessfulEmptyFileReplacesPreviousSymbols(t *testing.T) {
	p := newRefreshProject(t)
	p.write(t, "source.py", "def target():\n    pass\n")
	if err := p.build(t, false); err != nil {
		t.Fatal(err)
	}
	p.write(t, "source.py", "# now a valid empty supported file\n")
	if err := p.build(t, false); err != nil {
		t.Fatal(err)
	}
	symbols, err := p.database.GetFileSymbols(filepath.Join(p.root, "source.py"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := p.database.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(symbols) != 0 || snapshot.Counts.Files != 1 || snapshot.Counts.Symbols != 0 {
		t.Fatalf("empty file lost or retained old symbols: %+v %+v", symbols, snapshot)
	}
	if err := p.build(t, false); err != nil {
		t.Fatal(err)
	}
	after, err := p.database.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if after.ID != snapshot.ID {
		t.Fatal("empty file was not reusable")
	}
}

func TestAllSymbolProvidersFailRetainsSnapshot(t *testing.T) {
	p := newRefreshProject(t)
	p.write(t, "source.go", "package fixture\nfunc Target() {}\n")
	if err := p.build(t, false); err != nil {
		t.Fatal(err)
	}
	before, err := p.database.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	idx := NewIndexer(p.cfg, p.database, p.root)
	idx.SetProgressWriter(io.Discard)
	defer idx.Close()
	err = idx.IndexProject(context.Background(), []FileInfo{{Path: filepath.Join(p.root, "source.go"), RelPath: "source.go", Language: "no-supported-provider"}}, false)
	if err == nil {
		t.Fatal("all-provider failure returned success")
	}
	after, err := p.database.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := p.database.LatestAttempt()
	if err != nil {
		t.Fatal(err)
	}
	if after.ID != before.ID || attempt.Outcome != "failed" || attempt.FinishedAt == nil || attempt.Error == "" {
		t.Fatalf("failure lost publication/report: %+v %+v", after, attempt)
	}
}

func TestSuccessfulEmptyLSPResponseIsNotFailure(t *testing.T) {
	p := newTransportProject(t, "refresh/before.go", "source.go")
	fixture := filepath.Join(t.TempDir(), "empty.json")
	if err := os.WriteFile(fixture, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	p.cfg.LSP["go"] = config.LSPConfig{Command: scriptedPeer(t), Args: []string{"-responses", fixture}}
	if _, err := p.index(context.Background(), t); err != nil {
		t.Fatal(err)
	}
	attempt, err := p.database.LatestAttempt()
	if err != nil {
		t.Fatal(err)
	}
	emptySuccess := false
	for _, phase := range attempt.Phases {
		if phase.Phase == "symbols" && phase.Provider == "lsp" && phase.Status == "complete" && phase.Records == 0 {
			emptySuccess = true
		}
	}
	if !emptySuccess {
		t.Fatalf("successful empty semantic response lost: %+v", attempt.Phases)
	}
}

// The empty supported-source project is a coherent complete publication, not
// an all-provider failure, and does not acquire a new timestamp on a no-op.
func TestEmptyProjectQuality(t *testing.T) {
	p := newRefreshProject(t)
	if err := p.build(t, false); err != nil {
		t.Fatal(err)
	}
	snapshot, err := p.database.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Quality != "complete" || snapshot.Counts.Files != 0 {
		t.Fatalf("empty publication=%+v", snapshot)
	}
}
