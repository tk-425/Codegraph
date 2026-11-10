package main

import (
	"bytes"
	"crypto/sha256"
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
	return newSyntaxProjectAt(t, t.TempDir(), files)
}

func newSyntaxProjectAt(t *testing.T, root string, files map[string]string) string {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
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

func fileDigest(t *testing.T, path string) [32]byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(content)
}

// SC12 (D06): a graph from an earlier index format is never read or extended
// as if current; only build --force replaces it.
func TestLegacyIndexFormatRequiresExplicitRebuild(t *testing.T) {
	root := newSyntaxProject(t, map[string]string{
		"source.go": "package fixture\n\nfunc Target() {}\n\nfunc Caller() { Target() }\n",
	})
	requireBuild(t, root)
	dbPath := filepath.Join(root, ".codegraph", "graphs", "codegraph.db")
	conn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec("PRAGMA user_version = 0"); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	legacy := fileDigest(t, dbPath)

	for _, args := range [][]string{
		{"callers", "Target", "--json"},
		{"search", "Target", "--json"},
		{"stats", "--json"},
		{"build", "--json"},
	} {
		requireErrorCode(t, runCLI(t, root, args...), "index_format_incompatible")
		if fileDigest(t, dbPath) != legacy {
			t.Fatalf("%v modified the legacy database", args)
		}
	}
	if human := runCLI(t, root, "callers", "Target"); human.exit == 0 || !bytes.Contains(human.stderr, []byte("build --force")) {
		t.Errorf("human query on legacy index: exit=%d stderr=%s", human.exit, human.stderr)
	}

	requireBuild(t, root, "--force")
	if got := userVersion(t, root); got != db.FormatVersion {
		t.Errorf("after force rebuild user_version=%d, want %d", got, db.FormatVersion)
	}
	if found := requireOneEnvelope(t, runCLI(t, root, "search", "Target", "--json", "--exact")); found.Count != 1 {
		t.Errorf("search after rebuild = %d, want 1", found.Count)
	}
}

// T40: a project whose path needs percent-encoding builds and answers queries.
func TestAwkwardProjectPathBuildsAndQueries(t *testing.T) {
	root := newSyntaxProjectAt(t, filepath.Join(t.TempDir(), "a ?#é𝔘 b"), map[string]string{
		"dir #1/source.go": "package fixture\n\nfunc Target() {}\n",
	})
	requireBuild(t, root)
	result := runCLI(t, root, "search", "Target", "--json", "--exact")
	if found := requireOneEnvelope(t, result); found.Count != 1 {
		t.Fatalf("search in awkward path found %d: %s; stderr=%s", found.Count, result.stdout, result.stderr)
	}
	if !bytes.Contains(result.stdout, []byte("dir #1/source.go")) {
		t.Errorf("result path not preserved: %s", result.stdout)
	}
}
