package main

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

// S4b.1 tracer: the real CLI preserves stored identity and provenance without
// changing the published database while answering a filtered search.
func TestSearchCLITracer(t *testing.T) {
	root := newSyntaxProject(t, map[string]string{
		"source.go": "package fixture\n\nfunc SearchTracer() {}\n",
	})
	requireBuild(t, root)
	dbPath := filepath.Join(root, ".codegraph", "graphs", "codegraph.db")
	before := fileDigest(t, dbPath)
	result := runCLI(t, root, "search", "SearchTracer", "--exact", "--kind=function", "--lang=go", "--json")
	if result.exit != 0 {
		t.Fatalf("search exit=%d stdout=%s stderr=%s", result.exit, result.stdout, result.stderr)
	}
	env := requireOneEnvelope(t, result)
	if env.Count != 1 {
		t.Fatalf("count=%d, want one stored symbol: %s", env.Count, result.stdout)
	}
	var record struct {
		ID, Name, Source, Origin, Kind, Language string
	}
	if err := json.Unmarshal(env.Results[0], &record); err != nil {
		t.Fatal(err)
	}
	if record.ID == "" || record.Name != "SearchTracer" || record.Source != "tree-sitter" || record.Origin != "stored" || record.Kind != "function" || record.Language != "go" {
		t.Fatalf("stored result lost identity, filters or provenance: %+v", record)
	}
	if fileDigest(t, dbPath) != before {
		t.Fatal("search modified the published database")
	}
}

func TestSearchFilteredMissReportsSkippedFallback(t *testing.T) {
	root := newSyntaxProject(t, map[string]string{
		"source.go": "package fixture\n// OnlyTextMarker is not a declaration.\n",
	})
	requireBuild(t, root)
	for _, filter := range []string{"--exact", "--kind=class"} {
		result := runCLI(t, root, "search", "OnlyTextMarker", filter, "--json")
		if result.exit != 0 {
			t.Fatalf("filtered miss exit=%d: %s", result.exit, result.stderr)
		}
		if env := requireOneEnvelope(t, result); env.Count != 0 || len(env.Errors) != 0 {
			t.Fatalf("filtered miss fabricated symbol results or failed: %s", result.stdout)
		}
		var env struct {
			Metadata struct {
				Search struct {
					Skipped []struct{ Tier, Reason string } `json:"skipped_tiers"`
				}
			}
		}
		if err := json.Unmarshal(result.stdout, &env); err != nil {
			t.Fatal(err)
		}
		if len(env.Metadata.Search.Skipped) != 1 || env.Metadata.Search.Skipped[0].Tier != "ripgrep" || env.Metadata.Search.Skipped[0].Reason == "" {
			t.Fatalf("missing structured skip: %s", result.stdout)
		}
		if !strings.Contains(string(result.stderr), "skipped") {
			t.Fatalf("missing stderr skip notice: %s", result.stderr)
		}
		human := runCLI(t, root, "search", "OnlyTextMarker", filter)
		if human.exit != 0 || !strings.Contains(string(human.stderr), "skipped") || !strings.Contains(string(human.stdout), "No results") {
			t.Fatalf("human filtered miss: exit=%d stdout=%s stderr=%s", human.exit, human.stdout, human.stderr)
		}
	}
}

func TestSearchRejectsNegativeLimitBeforeSetup(t *testing.T) {
	root := t.TempDir()
	requireErrorCode(t, runCLI(t, root, "search", "x", "--limit=-1", "--json"), "invalid_arguments")
	human := runCLI(t, root, "search", "x", "--limit=-1")
	if human.exit == 0 || !strings.Contains(string(human.stderr), "limit") {
		t.Fatalf("negative limit: exit=%d stdout=%s stderr=%s", human.exit, human.stdout, human.stderr)
	}
	if _, err := os.Stat(filepath.Join(root, ".codegraph")); !os.IsNotExist(err) {
		t.Fatalf("invalid argument created project state: %v", err)
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

	// Health is diagnostic: it exits 0 but must not call this database healthy.
	health := runCLI(t, root, "health", "--json")
	if health.exit != 0 {
		t.Errorf("health exit=%d, want 0", health.exit)
	}
	databaseOK, sawDatabase := false, false
	for _, raw := range requireOneEnvelope(t, health).Results {
		var record struct {
			Category string `json:"category"`
			OK       bool   `json:"ok"`
			Detail   string `json:"detail"`
		}
		if err := json.Unmarshal(raw, &record); err != nil {
			t.Fatal(err)
		}
		switch record.Category {
		case "database":
			databaseOK, sawDatabase = record.OK, true
			if !strings.Contains(record.Detail, "build --force") {
				t.Errorf("health database detail=%q, want build --force guidance", record.Detail)
			}
		case "stats":
			t.Errorf("health reported stats from a legacy index: %s", raw)
		}
	}
	if !sawDatabase || databaseOK {
		t.Errorf("health database record present=%v ok=%v, want ok=false; stdout=%s", sawDatabase, databaseOK, health.stdout)
	}
	if human := runCLI(t, root, "health"); human.exit != 0 || bytes.Contains(human.stdout, []byte("accessible")) || !bytes.Contains(human.stdout, []byte("build --force")) {
		t.Errorf("human health on legacy index: exit=%d stdout=%s", human.exit, human.stdout)
	}
	if fileDigest(t, dbPath) != legacy {
		t.Fatal("health modified the legacy database")
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

// T23: a forced rebuild of a legacy index that fails before it can publish
// leaves the legacy database byte-for-byte intact — data and version stamp
// — because nothing writes a legacy graph until a publication succeeds.
func TestFailedForceRebuildPreservesLegacyIndex(t *testing.T) {
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

	// A rebuild that cannot finish: its source is unreadable, so the
	// attempt fails before any publication could run.
	source := filepath.Join(root, "source.go")
	if err := os.Chmod(source, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(source, 0o644) })
	result := runCLI(t, root, "build", "--json", "--force")
	if result.exit == 0 {
		t.Fatalf("T23: failed rebuild reported success: %s", result.stdout)
	}

	if fileDigest(t, dbPath) != legacy {
		t.Error("T23: failed forced rebuild modified the legacy database")
	}
	if got := userVersion(t, root); got != 0 {
		t.Errorf("T23: legacy version stamp after failed rebuild = %d, want 0", got)
	}
	// The transition outcome stays explicit: the legacy index still refuses.
	requireErrorCode(t, runCLI(t, root, "search", "Target", "--json"), "index_format_incompatible")
}
