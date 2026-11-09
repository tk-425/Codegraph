package indexer

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tk-425/Codegraph/internal/config"
	"github.com/tk-425/Codegraph/internal/db"
	"github.com/tk-425/Codegraph/internal/lsp"
)

type transportProject struct {
	root     string
	database *db.Manager
	cfg      *config.Config
}

func newTransportProject(t *testing.T, fixture, name string) *transportProject {
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
	content, err := os.ReadFile(filepath.Join("..", "..", "testdata", "characterization", fixture))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, name), content, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	for language := range cfg.LSP {
		cfg.LSP[language] = config.LSPConfig{Command: filepath.Join(root, "unavailable-lsp")}
	}
	return &transportProject{root, database, cfg}
}

func (p *transportProject) index(ctx context.Context, t *testing.T) (*Indexer, error) {
	t.Helper()
	ignorePath := filepath.Join(p.root, ".cgignore")
	if err := os.WriteFile(ignorePath, []byte(".codegraph/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scanner, err := NewScanner(p.root, ignorePath)
	if err != nil {
		t.Fatal(err)
	}
	files, err := scanner.Scan(ctx)
	if err != nil {
		return nil, err
	}
	idx := NewIndexer(p.cfg, p.database, p.root)
	idx.SetProgressWriter(io.Discard)
	t.Cleanup(func() { _ = idx.Close() })
	return idx, idx.IndexProject(ctx, files, false)
}

func TestIndexProjectDoesNotWaitForAbsentServer(t *testing.T) {
	p := newTransportProject(t, "refresh/before.go", "source.rs")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := p.index(ctx, t); err != nil {
		t.Fatalf("fallback-only build with an absent server: %v (it must not sleep for a server that never started)", err)
	}
}

func TestIndexProjectStopsWhenCancelled(t *testing.T) {
	p := newTransportProject(t, "refresh/before.go", "source.go")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.index(ctx, t); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the build to stop with context.Canceled", err)
	}
}

func TestIndexProjectReportsFailedRequestAmongSuccesses(t *testing.T) {
	p := newTransportProject(t, "calls/references.go", "references.go")
	peer := filepath.Join(t.TempDir(), "scripted-lsp")
	fixtureRoot, err := filepath.Abs(filepath.Join("..", "..", "testdata", "characterization", "lsp"))
	if err != nil {
		t.Fatal(err)
	}
	buildCtx, cancelBuild := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancelBuild()
	if output, err := exec.CommandContext(buildCtx, "go", "build", "-o", peer, fixtureRoot).CombinedOutput(); err != nil {
		t.Fatalf("build protocol peer: %v\n%s", err, output)
	}
	p.cfg.LSP["go"] = config.LSPConfig{Command: peer, Args: []string{"-responses", filepath.Join(fixtureRoot, "request-failure.json")}}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	idx, err := p.index(ctx, t)
	if err != nil {
		t.Fatal(err)
	}
	var found []lsp.LSPDiagnostic
	for _, diagnostic := range idx.Diagnostics() {
		if diagnostic.Category == lsp.RequestFailed {
			found = append(found, diagnostic)
		}
	}
	if len(found) != 1 {
		t.Fatalf("T28: request_failed diagnostics = %+v, want one for the calls phase", idx.Diagnostics())
	}
	if found[0].Language != "go" || found[0].Executable != peer || !strings.Contains(found[0].Reason, "calls") || !strings.Contains(found[0].Reason, "scripted internal error") {
		t.Errorf("T28: diagnostic %+v does not attribute the failure to the go calls phase", found[0])
	}
}
