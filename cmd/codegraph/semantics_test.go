package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/tk-425/Codegraph/internal/config"
	"github.com/tk-425/Codegraph/internal/db"
)

// newSyntaxProject creates an initialized project whose language servers are
// all absent, so builds use tree-sitter only.
func newSyntaxProject(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	cfg := config.DefaultConfig()
	for language := range cfg.LSP {
		cfg.LSP[language] = config.LSPConfig{Command: filepath.Join(root, "unavailable-lsp")}
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".codegraph", ".cgignore"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func requireBuild(t *testing.T, root string, args ...string) {
	t.Helper()
	result := runCLI(t, root, append([]string{"build", "--json"}, args...)...)
	if result.exit != 0 {
		t.Fatalf("build exit=%d; stdout=%s; stderr=%s", result.exit, result.stdout, result.stderr)
	}
}

func userVersion(t *testing.T, root string) int {
	t.Helper()
	conn, err := sql.Open("sqlite3", filepath.Join(root, ".codegraph", "graphs", "codegraph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var version int
	if err := conn.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	return version
}

// Tracer: a build through the CLI stores receiver-distinct identities in the
// current format, and a query reads both back.
func TestBuildStoresReceiverMethodsAsDistinctSymbols(t *testing.T) {
	root := newSyntaxProject(t, map[string]string{
		"receivers.go": "package fixture\n\ntype First struct{}\ntype Second struct{}\n\nfunc (First) Run() {}\n\nfunc (*Second) Run() {}\n",
	})
	requireBuild(t, root)

	result := runCLI(t, root, "search", "Run", "--json", "--exact")
	if result.exit != 0 {
		t.Fatalf("search exit=%d; stderr=%s", result.exit, result.stderr)
	}
	if found := requireOneEnvelope(t, result); found.Count != 2 {
		t.Errorf("search Run found %d symbols, want 2 (one per receiver): %s", found.Count, result.stdout)
	}
	if got := userVersion(t, root); got != db.FormatVersion {
		t.Errorf("user_version=%d, want %d", got, db.FormatVersion)
	}
}
