package indexer

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/tk-425/Codegraph/internal/db"
)

// syntaxGraph indexes symbols, calls and hierarchy of one file with
// tree-sitter only.
func syntaxGraph(t *testing.T, name, language, source string) (*db.Manager, FileInfo) {
	t.Helper()
	database, file := syntaxIndex(t, name, language, source)
	root := filepath.Dir(file.Path)
	if _, err := NewCallExtractor(database, root).ExtractCalls(context.Background(), file); err != nil {
		t.Fatal(err)
	}
	if _, err := NewHierarchyIndexer(database, nil, root).IndexHierarchyTreeSitter(context.Background(), file); err != nil {
		t.Fatal(err)
	}
	return database, file
}

func callersOf(t *testing.T, database *db.Manager, name string) []db.CallerInfo {
	t.Helper()
	callers, err := database.GetCallers(name, nil)
	if err != nil {
		t.Fatal(err)
	}
	return callers
}

// T15: a call inside a nested function belongs to the innermost function.
func TestSyntaxCallerIsInnermostFunction(t *testing.T) {
	database, _ := syntaxGraph(t, "nested.py", "python", `def outer():
    def inner():
        target()
    inner()

def target():
    pass
`)
	callers := callersOf(t, database, "target")
	if len(callers) != 1 || callers[0].Name != "inner" || callers[0].Scope != "outer" {
		t.Fatalf("T15: target callers = %+v, want outer.inner", callers)
	}
	if callers := callersOf(t, database, "inner"); len(callers) != 1 || callers[0].Name != "outer" {
		t.Errorf("T15: inner callers = %+v, want outer", callers)
	}
}

// T15: recursion on the declaration's own line, and two sites on one line.
func TestSyntaxCallSitesOnOneLineStayDistinct(t *testing.T) {
	database, _ := syntaxGraph(t, "lines.go", "go", `package fixture

func fact(n int) int { if n < 2 { return 1 }; return n * fact(n-1) }

func target() {}

func caller() { target(); target() }
`)
	recursion := callersOf(t, database, "fact")
	if len(recursion) != 1 || recursion[0].Name != "fact" || recursion[0].CallLine != 3 {
		t.Errorf("T15: same-line recursion = %+v, want fact calling itself on line 3", recursion)
	}
	sites := callersOf(t, database, "target")
	if len(sites) != 2 || sites[0].CallLine != 7 || sites[1].CallLine != 7 || sites[0].CallColumn == sites[1].CallColumn {
		t.Errorf("T15: two calls on line 7 = %+v, want two distinct columns", sites)
	}
	if len(sites) == 2 && (sites[0].CallColumn != 16 || sites[1].CallColumn != 26) {
		t.Errorf("call sites at columns %d and %d, want the callee names at 16 and 26", sites[0].CallColumn, sites[1].CallColumn)
	}
}

func TestSyntaxHierarchyChildIsTheIndexedType(t *testing.T) {
	database, _ := syntaxGraph(t, "parents.py", "python", "class Base:\n    pass\n\nclass Derived(Base):\n    pass\n")
	bases := symbolsNamed(t, database, "Base")
	if len(bases) != 1 {
		t.Fatalf("Base symbols = %+v", bases)
	}
	children, err := database.GetImplementations(bases[0].ID)
	if err != nil || len(children) != 1 || children[0].Name != "Derived" {
		t.Errorf("Derived does not extend the indexed Base: %+v, %v", children, err)
	}
}

// TSX files parse with the TSX grammar, so calls inside JSX are found.
func TestTSXCallsUseTheTSXGrammar(t *testing.T) {
	database, _ := syntaxGraph(t, "hello.tsx", "typescriptreact", `function greet(): string { return "hi" }

function Hello() {
  return <div className="x">{greet()}</div>
}
`)
	if callers := callersOf(t, database, "greet"); len(callers) != 1 || callers[0].Name != "Hello" {
		t.Errorf("greet callers in TSX = %+v, want Hello", callers)
	}
}
