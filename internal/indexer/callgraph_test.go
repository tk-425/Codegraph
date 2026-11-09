package indexer

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tk-425/Codegraph/internal/db"
	"github.com/tk-425/Codegraph/internal/lsp"
)

// serverCalls indexes one file's symbols with tree-sitter, then runs the
// language-server call pass alone against a responses fixture, returning
// how many calls it stored.
func serverCalls(t *testing.T, fixture, name, language, responses string) (*db.Manager, int) {
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
	count, err := NewCallGraphIndexer(p.database, manager, p.root).IndexCallGraph(ctx, language)
	if err != nil {
		t.Fatal(err)
	}
	return p.database, count
}

// T15: references on the declaration's line are kept, and two calls on one
// line are two sites.
func TestReferenceCallsOnOneLineStayDistinct(t *testing.T) {
	database, count := serverCalls(t, "calls/lines.go", "lines.go", "go", "lines.json")
	if count != 3 {
		t.Fatalf("server pass stored %d calls, want 3", count)
	}
	recursion := callersOf(t, database, "fact")
	if len(recursion) != 1 || recursion[0].Name != "fact" || recursion[0].CallLine != 3 || recursion[0].CallColumn != 57 {
		t.Errorf("T15: same-line recursion = %+v, want fact at 3:57", recursion)
	}
	sites := callersOf(t, database, "target")
	if len(sites) != 2 || sites[0].CallColumn != 16 || sites[1].CallColumn != 26 {
		t.Errorf("T15: calls on line 7 = %+v, want columns 16 and 26", sites)
	}
}

// T15: a reference inside a nested function belongs to the innermost one.
func TestReferenceCallerIsInnermostFunction(t *testing.T) {
	database, count := serverCalls(t, "calls/nested.py", "nested.py", "python", "nested.json")
	if count != 2 {
		t.Fatalf("server pass stored %d calls, want 2", count)
	}
	if callers := callersOf(t, database, "target"); len(callers) != 1 || callers[0].Name != "inner" {
		t.Errorf("T15: target callers = %+v, want inner", callers)
	}
	if callers := callersOf(t, database, "inner"); len(callers) != 1 || callers[0].Name != "outer" {
		t.Errorf("T15: inner callers = %+v, want outer", callers)
	}
}

// I06: with no call syntax to check a reference against, it is recorded as
// unvalidated and never stored as a call.
func TestUncheckableReferencesAreRecordedNotStored(t *testing.T) {
	database, count := serverCalls(t, "calls/unvalidated.c", "unvalidated.c", "c", "unvalidated.json")
	if count != 0 || len(callersOf(t, database, "target")) != 0 {
		t.Fatalf("unvalidated reference stored as a call (count %d)", count)
	}
	if sites := unresolvedNamed(t, database, "target"); len(sites) != 1 || sites[0].Reason != reasonUnvalidated || sites[0].Line != 2 {
		t.Errorf("unvalidated site not recorded: %+v", sites)
	}
}
