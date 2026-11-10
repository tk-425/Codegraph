package indexer

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tk-425/Codegraph/internal/db"
	"github.com/tk-425/Codegraph/internal/lsp"
)

// serverHierarchy indexes one file's symbols with tree-sitter, then runs the
// language-server hierarchy pass alone against a responses fixture,
// returning how many relationships it stored.
func serverHierarchy(t *testing.T, fixture, name, language, responses string) (*db.Manager, int) {
	t.Helper()
	p := newTransportProject(t, fixture, name)
	file := FileInfo{Path: filepath.Join(p.root, name), RelPath: name, Language: language}
	if _, err := NewTreeSitterIndexer(p.database, p.root).IndexFile(context.Background(), file); err != nil {
		t.Fatal(err)
	}
	useScriptedServer(t, p.cfg, language, responses)
	manager := lsp.NewManager(p.cfg, lsp.NewRoot(p.root).URI())
	t.Cleanup(func() { _ = manager.ShutdownAll() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	count, err := NewHierarchyIndexer(p.database, manager, p.root).IndexHierarchyLSP(ctx, language)
	if err != nil {
		t.Fatal(err)
	}
	return p.database, count
}

// T16: a parent declared outside the project is recorded as external, with
// the child as its local endpoint, and never stored as an edge.
func TestExternalHierarchyParentIsRecordedNotStored(t *testing.T) {
	database, count := serverHierarchy(t, "hierarchy/parents.py", "parents.py", "python", "hierarchy-external.json")
	if count != 0 {
		t.Fatalf("hierarchy pass stored %d relationships, want 0", count)
	}
	if children, err := database.GetImplementationsByName("Protocol"); err != nil || len(children) != 0 {
		t.Fatalf("external parent has stored children %+v (err %v)", children, err)
	}
	sites := unresolvedNamed(t, database, "Protocol")
	if len(sites) != 1 {
		t.Fatalf("T16: external parent sites = %+v, want one", sites)
	}
	if site := sites[0]; site.Reason != reasonExternal || site.Kind != "implements" || site.FromID != "python:parents.py#Derived" {
		t.Errorf("T16: external parent site = %+v, want external implements from Derived", site)
	}
}
