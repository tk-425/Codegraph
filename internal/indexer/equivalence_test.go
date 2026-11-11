package indexer

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// The S3 exit gate: an incremental refresh and a full rebuild from the same
// inputs must agree on symbols and relationships, ignoring publication
// timestamps (fix spec R01 acceptance, T01-T08, T19-T23 evidence base).

// graphFingerprint renders the published graph as comparable text:
// every symbol, call, type relationship, unresolved site and manifest row,
// with timestamps (created_at, mod_time, extracted_at, updated_at) excluded.
func graphFingerprint(t *testing.T, dbPath string) string {
	t.Helper()
	conn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	var out string
	rows, err := conn.Query(`SELECT id, name, kind, file, line, column, start_line, start_column,
		COALESCE(end_line, -1), COALESCE(end_column, -1), COALESCE(scope, ''), COALESCE(signature, ''),
		language, source FROM symbols ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id, name, kind, file, scope, signature, language, source string
		var line, column, startLine, startColumn, endLine, endColumn int
		if err := rows.Scan(&id, &name, &kind, &file, &line, &column, &startLine, &startColumn, &endLine, &endColumn, &scope, &signature, &language, &source); err != nil {
			t.Fatal(err)
		}
		out += fmt.Sprintf("S %s %s %s %s %d:%d %d:%d %d:%d %q %q %s %s\n",
			id, name, kind, filepath.Base(file), line, column, startLine, startColumn, endLine, endColumn, scope, signature, language, source)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	rows, err = conn.Query(`SELECT caller_id, callee_id, file, line, column FROM calls
		ORDER BY caller_id, callee_id, file, line, column`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var caller, callee, file string
		var line, column int
		if err := rows.Scan(&caller, &callee, &file, &line, &column); err != nil {
			t.Fatal(err)
		}
		out += fmt.Sprintf("C %s -> %s %s %d:%d\n", caller, callee, filepath.Base(file), line, column)
	}
	rows.Close()

	rows, err = conn.Query(`SELECT child_id, parent_id, relationship FROM type_hierarchy
		ORDER BY child_id, parent_id, relationship`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var child, parent, relationship string
		if err := rows.Scan(&child, &parent, &relationship); err != nil {
			t.Fatal(err)
		}
		out += fmt.Sprintf("H %s -> %s %s\n", child, parent, relationship)
	}
	rows.Close()

	rows, err = conn.Query(`SELECT kind, language, from_id, target_name, file, line, column, reason, candidates
		FROM unresolved_relationships ORDER BY kind, file, line, column, from_id, target_name`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var kind, language, from, target, file, reason string
		var line, column, candidates int
		if err := rows.Scan(&kind, &language, &from, &target, &file, &line, &column, &reason, &candidates); err != nil {
			t.Fatal(err)
		}
		out += fmt.Sprintf("U %s %s %s %q %s %d:%d %s %d\n", kind, language, from, target, filepath.Base(file), line, column, reason, candidates)
	}
	rows.Close()

	rows, err = conn.Query(`SELECT path, language FROM file_meta ORDER BY path`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var path, language string
		if err := rows.Scan(&path, &language); err != nil {
			t.Fatal(err)
		}
		out += fmt.Sprintf("M %s %s\n", filepath.Base(path), language)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// An incremental refresh through a sequence of edits, renames, deletions,
// additions and ignore changes must land exactly where a full rebuild from
// the same final inputs lands.
func TestIncrementalRefreshMatchesFullRebuild(t *testing.T) {
	p := newRefreshProject(t)

	initial := map[string]string{
		"a.go": "package main\n\nfunc Target() {}\n\nfunc Local() { Target() }\n",
		"b.go": "package main\n\nfunc UsesTarget() { Target() }\n",
		"c.py": "class Worker:\n    def run(self):\n        pass\n\ndef main():\n    w = Worker()\n    w.run()\n",
	}
	writeFiles(t, p.root, initial)
	if err := p.build(t, true); err != nil {
		t.Fatal(err)
	}

	mutations := []struct {
		name  string
		apply func(t *testing.T)
	}{
		{"edit renames a function", func(t *testing.T) {
			writeFiles(t, p.root, map[string]string{
				"a.go": "package main\n\nfunc Renamed() {}\n\nfunc Local() { Renamed() }\n",
				"b.go": "package main\n\nfunc UsesTarget() { Renamed() }\n",
			})
		}},
		{"delete a language's last file", func(t *testing.T) {
			if err := os.Remove(filepath.Join(p.root, "c.py")); err != nil {
				t.Fatal(err)
			}
		}},
		{"add a new file", func(t *testing.T) {
			writeFiles(t, p.root, map[string]string{
				"d.go": "package main\n\nfunc Added() { Renamed() }\n",
			})
		}},
		{"rename a file", func(t *testing.T) {
			if err := os.Rename(filepath.Join(p.root, "b.go"), filepath.Join(p.root, "b2.go")); err != nil {
				t.Fatal(err)
			}
		}},
		{"ignore then unignore a file", func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(p.root, ".cgignore"), []byte("d.go\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := p.build(t, false); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(p.root, ".cgignore"), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, mutation := range mutations {
		mutation.apply(t)
		if err := p.build(t, false); err != nil {
			t.Fatalf("%s: incremental build failed: %v", mutation.name, err)
		}
	}

	incremental := graphFingerprint(t, filepath.Join(p.root, ".codegraph", "graph.db"))

	// A full rebuild from the same final inputs.
	full := newRefreshProject(t)
	writeFiles(t, full.root, map[string]string{
		"a.go":  "package main\n\nfunc Renamed() {}\n\nfunc Local() { Renamed() }\n",
		"b2.go": "package main\n\nfunc UsesTarget() { Renamed() }\n",
		"d.go":  "package main\n\nfunc Added() { Renamed() }\n",
	})
	if err := full.build(t, true); err != nil {
		t.Fatal(err)
	}
	rebuilt := graphFingerprint(t, filepath.Join(full.root, ".codegraph", "graph.db"))

	if incremental != rebuilt {
		t.Fatalf("incremental refresh and full rebuild disagree:\n--- incremental ---\n%s\n--- full rebuild ---\n%s", incremental, rebuilt)
	}

	// A repeated refresh is a no-op: same graph, no count inflation.
	before := graphFingerprint(t, filepath.Join(p.root, ".codegraph", "graph.db"))
	if err := p.build(t, false); err != nil {
		t.Fatal(err)
	}
	if after := graphFingerprint(t, filepath.Join(p.root, ".codegraph", "graph.db")); after != before {
		t.Fatalf("repeat refresh changed the graph:\n%s\n--- after ---\n%s", before, after)
	}
}

// The empty project is a coherent snapshot, not stale success: after the
// last source is excluded, a full rebuild of nothing and an incremental
// refresh of nothing agree on an empty graph.
func TestEmptyProjectIncrementalMatchesFull(t *testing.T) {
	incremental := newRefreshProject(t)
	writeFiles(t, incremental.root, map[string]string{
		"a.go": "package main\n\nfunc Target() {}\n",
	})
	if err := incremental.build(t, true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(incremental.root, ".cgignore"), []byte("*.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := incremental.build(t, false); err != nil {
		t.Fatal(err)
	}
	got := graphFingerprint(t, filepath.Join(incremental.root, ".codegraph", "graph.db"))

	full := newRefreshProject(t)
	writeFiles(t, full.root, map[string]string{
		"a.go": "package main\n\nfunc Target() {}\n",
	})
	if err := os.WriteFile(filepath.Join(full.root, ".cgignore"), []byte("*.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := full.build(t, true); err != nil {
		t.Fatal(err)
	}
	want := graphFingerprint(t, filepath.Join(full.root, ".codegraph", "graph.db"))

	if got != want {
		t.Fatalf("empty-project snapshots disagree:\n--- incremental ---\n%s\n--- full ---\n%s", got, want)
	}
	if got != "" {
		t.Fatalf("empty project kept graph content:\n%s", got)
	}
}
