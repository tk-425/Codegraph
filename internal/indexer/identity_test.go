package indexer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/tk-425/Codegraph/internal/db"
)

// syntaxIndex indexes one source file with tree-sitter into a fresh database.
func syntaxIndex(t *testing.T, name, language, source string) (*db.Manager, FileInfo) {
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
	return database, file
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
	database, _ := syntaxIndex(t, "Calc.java", "java", `class Calc {
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
	database, _ := syntaxIndex(t, "nested.py", "python", `class Outer:
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

func TestSyntaxSignaturesRetainDeclarationHeaders(t *testing.T) {
	for _, tc := range []struct {
		name, language, filename, source, want string
	}{
		{"go", "go", "source.go", "package fixture\nfunc convert(\n input map[string]int,\n) (value int, ok bool) { return 0, false }", "func convert(\n input map[string]int,\n) (value int, ok bool)"},
		{"go-method", "go", "source.go", "package fixture\ntype T struct{}\nfunc (*T) convert(\n input int,\n) int { return input }", "func (*T) convert(\n input int,\n) int"},
		{"python", "python", "source.py", "def convert(\n value: int,\n) -> str:\n return str(value)\n", "def convert(\n value: int,\n) -> str:"},
		{"swift", "swift", "source.swift", "func convert(\n value: Int\n) -> String { return String(value) }", "func convert(\n value: Int\n) -> String"},
		{"typescript", "typescript", "source.ts", "function convert<T>(\n value: T,\n): {value: T} { return {value}; }", "function convert<T>(\n value: T,\n): {value: T}"},
		{"typescript-method", "typescript", "source.ts", "class C { convert(\n value: number\n): number { return value; } }", "convert(\n value: number\n): number"},
		{"javascript", "javascript", "source.js", "async function convert(\n value\n) { return value; }", "async function convert(\n value\n)"},
		{"tsx", "typescriptreact", "source.tsx", "function convert(\n value: string\n): JSX.Element { return <div>{value}</div>; }", "function convert(\n value: string\n): JSX.Element"},
		{"java", "java", "Source.java", "class C { public int convert(\n int value\n) throws Exception { return value; } }", "public int convert(\n int value\n) throws Exception"},
		{"java-bodyless", "java", "Source.java", "interface C { int convert(\n int value\n); }", "int convert(\n int value\n);"},
		{"rust", "rust", "source.rs", "fn convert(\n value: i32, callback: fn(i32) -> i32,\n) -> i32 { callback(value) }", "fn convert(\n value: i32, callback: fn(i32) -> i32,\n) -> i32"},
		{"rust-unit", "rust", "source.rs", "fn convert() { let value = 1; }", "fn convert()"},
		{"ocaml-binding", "ocaml", "source.ml", "let convert (x : int)\n (y : int) : int = x + y\n", "convert (x : int)\n (y : int) : int"},
		{"ocaml-function-type", "ocaml", "source.ml", "let convert : (int -> int) -> int = fun f -> f 1\n", "convert : (int -> int) -> int"},
		{"ocaml-val", "ocaml", "source.ml", "module type S = sig\nval convert : int ->\n string -> bool\nend\n", "val convert : int ->\n string -> bool"},
		{"c", "c", "source.c", "int convert(\n int value\n) { return value; }", "int convert(\n int value\n)"},
		{"cpp", "cpp", "source.cpp", "int convert(\n int value\n) noexcept { return value; }", "int convert(\n int value\n) noexcept"},
		{"csharp", "csharp", "Source.cs", "class C { public int convert(\n int value\n) { return value; } }", "public int convert(\n int value\n)"},
		{"csharp-expression", "csharp", "Source.cs", "class C { int convert(\n int value\n) => value + 1; }", "int convert(\n int value\n)"},
		{"csharp-bodyless", "csharp", "Source.cs", "abstract class C { public abstract int convert(\n int value\n); }", "public abstract int convert(\n int value\n);"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database, _ := syntaxIndex(t, tc.filename, tc.language, tc.source)
			symbols := symbolsNamed(t, database, "convert")
			if len(symbols) != 1 {
				t.Fatalf("want one stored declaration, got %+v", symbols)
			}
			if symbols[0].Signature != tc.want {
				t.Errorf("signature=%q, want %q", symbols[0].Signature, tc.want)
			}
			if symbols[0].Documentation != "" || symbols[0].Source != "tree-sitter" {
				t.Errorf("signature lost provider or fabricated documentation: %+v", symbols[0])
			}
		})
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
