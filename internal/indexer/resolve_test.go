package indexer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tk-425/Codegraph/internal/db"
)

// syntaxProject indexes several files with tree-sitter into one database,
// then extracts calls from each.
func syntaxProject(t *testing.T, files map[string]string) (*db.Manager, string) {
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
	var infos []FileInfo
	for name, source := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
		info := FileInfo{Path: path, RelPath: name, Language: languageOf(t, name)}
		if _, err := NewTreeSitterIndexer(database, root).IndexFile(context.Background(), info); err != nil {
			t.Fatal(err)
		}
		infos = append(infos, info)
	}
	for _, info := range infos {
		if _, err := NewCallExtractor(database, root).ExtractCalls(context.Background(), info); err != nil {
			t.Fatal(err)
		}
	}
	return database, root
}

func languageOf(t *testing.T, name string) string {
	t.Helper()
	switch filepath.Ext(name) {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".ts":
		return "typescript"
	}
	t.Fatalf("no language for %s", name)
	return ""
}

func unresolvedNamed(t *testing.T, database *db.Manager, name string) []db.Unresolved {
	t.Helper()
	all, err := database.GetUnresolved()
	if err != nil {
		t.Fatal(err)
	}
	var found []db.Unresolved
	for _, u := range all {
		if u.TargetName == name {
			found = append(found, u)
		}
	}
	return found
}

// T13: same-named candidates in other packages are ambiguous, not a guess.
func TestSameNamedCandidatesAreRecordedAmbiguous(t *testing.T) {
	database, _ := syntaxProject(t, map[string]string{
		"a/a.go": "package a\n\nfunc helper() {}\n",
		"b/b.go": "package b\n\nfunc helper() {}\n",
		"c/c.go": "package c\n\nfunc use() { helper() }\n",
	})
	if callers := callersOf(t, database, "helper"); len(callers) != 0 {
		t.Errorf("T13: ambiguous call was bound: %+v", callers)
	}
	sites := unresolvedNamed(t, database, "helper")
	if len(sites) != 1 || sites[0].Reason != reasonAmbiguous || sites[0].Candidates != 2 || sites[0].Kind != "call" {
		t.Errorf("T13: ambiguity not recorded: %+v", sites)
	}
}

// T13: the narrowest evidence wins — same file, then same Go package.
func TestNarrowestEvidenceResolvesTheCandidate(t *testing.T) {
	database, _ := syntaxProject(t, map[string]string{
		"x.py":     "def helper():\n    pass\n\ndef use():\n    helper()\n",
		"y.py":     "def helper():\n    pass\n",
		"p/one.go": "package p\n\nfunc use() { helper() }\n",
		"p/two.go": "package p\n\nfunc helper() {}\n",
		"q/q.go":   "package q\n\nfunc helper() {}\n",
	})
	for language, want := range map[string]string{"python": "x.py", "go": filepath.Join("p", "two.go")} {
		callees, err := database.GetCallees("use", []string{language})
		if err != nil {
			t.Fatal(err)
		}
		if len(callees) != 1 || !strings.HasSuffix(callees[0].File, string(filepath.Separator)+want) {
			t.Errorf("T13: %s use() bound to %+v, want helper in %s", language, callees, want)
		}
	}
}

// T13: a name defined only in another language family is never bound.
func TestNamesNeverBindAcrossLanguages(t *testing.T) {
	database, _ := syntaxProject(t, map[string]string{
		"only.ts": "function only() {}\n",
		"use.py":  "def use():\n    only()\n",
	})
	if callers := callersOf(t, database, "only"); len(callers) != 0 {
		t.Errorf("T13: Python call bound to a TypeScript function: %+v", callers)
	}
	if sites := unresolvedNamed(t, database, "only"); len(sites) != 1 || sites[0].Reason != reasonUnresolved {
		t.Errorf("T13: cross-language site not recorded unresolved: %+v", sites)
	}
}

// T13: self.m() binds to the caller's own class even when another class in
// the same file declares m.
func TestSelfCallBindsToTheOwningClass(t *testing.T) {
	database, _ := syntaxProject(t, map[string]string{
		"owner.py": "class A:\n    def run(self):\n        self.go()\n\n    def go(self):\n        pass\n\nclass B:\n    def go(self):\n        pass\n",
	})
	callees, err := database.GetCallees("run", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(callees) != 1 || callees[0].Scope != "A" {
		t.Errorf("T13: self.go() resolved to %+v, want A.go", callees)
	}
}

// T12: calls found by syntax attach to symbols the language server indexed.
func TestSyntaxCallsAttachToLSPSymbols(t *testing.T) {
	p := newTransportProject(t, "calls/mixed.go", "mixed.go")
	useScriptedServer(t, p.cfg, "go", "mixed-symbols.json")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := p.index(ctx, t); err != nil {
		t.Fatal(err)
	}
	targets := symbolsNamed(t, p.database, "Target")
	if len(targets) != 1 || targets[0].Source != "lsp" {
		t.Fatalf("want LSP-indexed Target, got %+v", targets)
	}
	callers := callersOf(t, p.database, "Target")
	if len(callers) != 1 || callers[0].Name != "Caller" || callers[0].Source != "lsp" {
		t.Errorf("T12: syntax call did not attach to LSP symbols: %+v", callers)
	}
}
