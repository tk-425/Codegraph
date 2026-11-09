package indexer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/tk-425/Codegraph/internal/db"
)

// syntaxIndex indexes one source file with tree-sitter into a fresh database.
func syntaxIndex(t *testing.T, name, language, source string) *db.Manager {
	t.Helper()
	root := t.TempDir()
	database, err := db.NewManager(filepath.Join(root, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := database.Initialize(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	file := FileInfo{Path: path, RelPath: name, Language: language}
	if _, err := NewTreeSitterIndexer(database, root).IndexFile(context.Background(), file); err != nil {
		t.Fatal(err)
	}
	return database
}

func symbolsNamed(t *testing.T, database *db.Manager, name string) []db.Symbol {
	t.Helper()
	symbols, err := database.GetSymbolByName(name, nil)
	if err != nil {
		t.Fatal(err)
	}
	return symbols
}

func TestOverloadsKeepDistinctIdentities(t *testing.T) {
	database := syntaxIndex(t, "Calc.java", "java", `class Calc {
    int add(int a) { return a; }
    int add(String s) { return 0; }
}
`)
	adds := symbolsNamed(t, database, "add")
	if len(adds) != 2 || adds[0].ID == adds[1].ID {
		t.Fatalf("T10: overloads collapsed: %+v", adds)
	}
	if adds[0].Line == adds[1].Line {
		t.Errorf("T10: overloads share a declaration line: %+v", adds)
	}
}

func TestNestedScopesKeepTheFullOwnerChain(t *testing.T) {
	database := syntaxIndex(t, "nested.py", "python", `class Outer:
    class Inner:
        def run(self):
            pass

class Other:
    class Inner:
        def run(self):
            pass
`)
	runs := symbolsNamed(t, database, "run")
	if len(runs) != 2 || runs[0].ID == runs[1].ID {
		t.Fatalf("T10: nested methods collapsed: %+v", runs)
	}
	scopes := map[string]bool{runs[0].Scope: true, runs[1].Scope: true}
	if !scopes["Outer.Inner"] || !scopes["Other.Inner"] {
		t.Errorf("T10: scope truncated, want Outer.Inner and Other.Inner: %+v", runs)
	}
}

func TestSameNameInDifferentLanguagesHasDistinctIdentities(t *testing.T) {
	key := symbolKey{Language: "c", RelPath: "x.h", Name: "f"}
	other := key
	other.Language = "cpp"
	if key.ID() == other.ID() {
		t.Errorf("language is not part of identity: %s", key.ID())
	}
}

func TestGoReceiverNamesNormalize(t *testing.T) {
	for _, name := range []string{"(First).Run", "(*First).Run", "(*First[K]).Run"} {
		receiver, method, ok := splitGoReceiver(name)
		if !ok || receiver != "First" || method != "Run" {
			t.Errorf("splitGoReceiver(%q) = %q, %q, %v", name, receiver, method, ok)
		}
	}
	if _, _, ok := splitGoReceiver("Run"); ok {
		t.Error("plain function name parsed as a method")
	}
	for _, expr := range []string{"First", "*First", "First[K]", "*First[K, V]"} {
		if got := goReceiverType(expr); got != "First" {
			t.Errorf("goReceiverType(%q) = %q", expr, got)
		}
	}
}
