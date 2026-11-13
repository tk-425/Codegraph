package cli

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/tk-425/Codegraph/internal/db"
)

// healthState builds one project state in a fresh cwd. It runs after PATH
// is pinned, so LSP results are the same on every machine.
type healthState struct {
	name  string
	setup func(t *testing.T, dir string)
	// records and human are the expected output with the project dir
	// written as $DIR. LSP records and lines are compared as sorted sets
	// because health ranges over a map.
	records []healthRecord
	human   []string
}

const healthDBRel = ".codegraph/graphs/codegraph.db"

func healthDBPath(dir string) string { return filepath.Join(dir, healthDBRel) }

func mkCodegraphDir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".codegraph", "graphs"), 0o755); err != nil {
		t.Fatalf("mkdir .codegraph/graphs: %v", err)
	}
}

func initHealthDB(t *testing.T, dir string) {
	t.Helper()
	mkCodegraphDir(t, dir)
	m, err := db.NewManager(healthDBPath(dir))
	if err != nil {
		t.Fatalf("db.NewManager: %v", err)
	}
	defer m.Close()
	if err := m.Initialize(); err != nil {
		t.Fatalf("db.Initialize: %v", err)
	}
}

// lspHealth is the LSP section for the default config when only gopls is
// on PATH.
func lspHealth() ([]healthRecord, []string) {
	missing := map[string]string{
		"python": "pyright-langserver", "typescript": "typescript-language-server",
		"typescriptreact": "typescript-language-server", "java": "jdtls",
		"swift": "sourcekit-lsp", "rust": "rust-analyzer", "ocaml": "ocamllsp",
	}
	detail := "command=\"gopls\"; args=[\"serve\"]; executable available; initialization unchecked; capabilities unchecked"
	records := []healthRecord{{Category: "lsp", Name: "go", OK: true, Detail: detail}}
	lines := []string{"   ✅ go: " + detail}
	for lang, command := range missing {
		args := `["--stdio"]`
		switch lang {
		case "java":
			args = `["-data" "$DIR/.codegraph/jdtls-workspace"]`
		case "swift", "rust", "ocaml":
			args = `[]`
		}
		detail := fmt.Sprintf("command=%q; args=%s; executable unavailable: exec: %q: executable file not found in $PATH; initialization unchecked; capabilities unchecked", command, args, command)
		records = append(records, healthRecord{Category: "lsp", Name: lang, OK: false, Detail: detail})
		lines = append(lines, "   ❌ "+lang+": "+detail)
	}
	return records, lines
}

func healthStates() []healthState {
	initialized := healthRecord{Category: "initialized", Name: "initialized", OK: true, Detail: ".codegraph/ directory exists"}
	config := healthRecord{Category: "config", Name: "config", OK: true, Detail: "config.toml loaded"}
	banner := []string{"🏥 Checking codegraph health...", ""}
	passed := append(append([]string{}, banner...), "✅ Initialized: .codegraph/ directory exists", "✅ Config: config.toml loaded")
	lspRecords, lspLines := lspHealth()

	return []healthState{
		{
			name:    "not initialized",
			setup:   func(t *testing.T, dir string) {},
			records: []healthRecord{{Category: "initialized", Name: "initialized", OK: false, Detail: "codegraph not initialized. Run 'codegraph init' first"}},
			human:   append(append([]string{}, banner...), "❌ Not initialized: Run 'codegraph init' first"),
		},
		{
			name: "bad config",
			setup: func(t *testing.T, dir string) {
				mkCodegraphDir(t, dir)
				if err := os.WriteFile(filepath.Join(dir, ".codegraph", "config.toml"), []byte("[lsp_timeouts]\nrequest_seconds = 0\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			records: []healthRecord{initialized, {Category: "config", Name: "config", OK: false, Detail: "invalid config: lsp_timeouts.request_seconds must be positive, got 0"}},
			human:   append(append(append([]string{}, banner...), "✅ Initialized: .codegraph/ directory exists"), "❌ Config error: invalid config: lsp_timeouts.request_seconds must be positive, got 0"),
		},
		{
			name:    "database missing",
			setup:   func(t *testing.T, dir string) { mkCodegraphDir(t, dir) },
			records: []healthRecord{initialized, config, {Category: "database", Name: "database", OK: false, Detail: "not found"}},
			human:   append(append([]string{}, passed...), "❌ Database: not found"),
		},
		{
			name: "database open failure",
			setup: func(t *testing.T, dir string) {
				initHealthDB(t, dir)
				if err := os.Chmod(healthDBPath(dir), 0); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(healthDBPath(dir), 0o644) })
			},
			records: []healthRecord{initialized, config, {Category: "database", Name: "database", OK: false, Detail: "failed to open database: unable to open database file: permission denied"}},
			human:   append(append([]string{}, passed...), "❌ Database error: failed to open database: unable to open database file: permission denied"),
		},
		{
			name: "not a database",
			setup: func(t *testing.T, dir string) {
				mkCodegraphDir(t, dir)
				if err := os.WriteFile(healthDBPath(dir), bytes.Repeat([]byte("x"), 4096), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			records: []healthRecord{initialized, config, {Category: "database", Name: "database", OK: false, Detail: "failed to open database: file is not a database"}},
			human:   append(append([]string{}, passed...), "❌ Database error: failed to open database: file is not a database"),
		},
		{
			name: "incompatible format",
			setup: func(t *testing.T, dir string) {
				initHealthDB(t, dir)
				raw, err := sql.Open("sqlite3", healthDBPath(dir))
				if err != nil {
					t.Fatal(err)
				}
				defer raw.Close()
				if _, err := raw.Exec("PRAGMA user_version = 0"); err != nil {
					t.Fatal(err)
				}
			},
			records: []healthRecord{initialized, config, {Category: "database", Name: "database", OK: false, Detail: (&db.IncompatibleFormatError{Found: 0}).Error()}},
			human:   append(append([]string{}, passed...), "❌ Database: "+(&db.IncompatibleFormatError{Found: 0}).Error()),
		},
		{
			name:  "healthy",
			setup: func(t *testing.T, dir string) { initHealthDB(t, dir) },
			records: append([]healthRecord{initialized, config,
				{Category: "database", Name: "database", OK: true, Detail: "accessible"},
				{Category: "stats", Name: "symbols", OK: true, Detail: "0"},
				{Category: "stats", Name: "calls", OK: true, Detail: "0"},
				{Category: "stats", Name: "files", OK: true, Detail: "0"},
				{Category: "snapshot", Name: "quality", OK: false, Detail: "unavailable; quality unknown; freshness unchecked"},
			}, lspRecords...),
			human: append(append(append([]string{}, passed...),
				"✅ Database: accessible", "", "📊 Statistics:",
				"   Symbols:   0", "   Calls:     0", "   Files:     0",
				"❌ Snapshot: unavailable; quality unknown; freshness unchecked",
				"", "🔧 LSP Servers:"), lspLines...),
		},
	}
}

// pinHealthEnv gives the test a fresh cwd and a PATH holding only a stub
// gopls, and returns the cwd as health sees it.
func pinHealthEnv(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "gopls"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Chdir(t.TempDir())
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// sortLSPTail sorts the trailing run of entries matched by isLSP, leaving
// everything before it in order.
func sortLSPTail[T any](items []T, isLSP func(T) bool, less func(a, b T) bool) []T {
	start := len(items)
	for start > 0 && isLSP(items[start-1]) {
		start--
	}
	tail := items[start:]
	sort.Slice(tail, func(i, j int) bool { return less(tail[i], tail[j]) })
	return items
}

func normalizeRecords(records []healthRecord, dir string) []healthRecord {
	for i := range records {
		records[i].Detail = strings.ReplaceAll(records[i].Detail, dir, "$DIR")
	}
	return sortLSPTail(records,
		func(r healthRecord) bool { return r.Category == "lsp" },
		func(a, b healthRecord) bool { return a.Name < b.Name })
}

func normalizeHuman(out, dir string) []string {
	lines := strings.Split(strings.TrimRight(stripANSI(out), "\n"), "\n")
	for i := range lines {
		lines[i] = strings.ReplaceAll(lines[i], dir, "$DIR")
	}
	return sortLSPTail(lines,
		func(l string) bool { return strings.HasPrefix(l, "   ✅ ") || strings.HasPrefix(l, "   ❌ ") },
		func(a, b string) bool { return strings.TrimLeft(a, " ✅❌") < strings.TrimLeft(b, " ✅❌") })
}

// TestHealthCharacterization pins health's output for every state its open
// sequence can end in, in both output modes. Every state is a diagnostic:
// exit 0 and no envelope errors.
func TestHealthCharacterization(t *testing.T) {
	for _, state := range healthStates() {
		t.Run(state.name+"/json", func(t *testing.T) {
			dir := pinHealthEnv(t)
			state.setup(t, dir)
			jsonOutputFlag = true
			t.Cleanup(func() { jsonOutputFlag = false })

			c, buf := freshCmdNoArgs(t, "health", runHealth)
			if err := c.RunE(c, nil); err != nil {
				t.Fatalf("runHealth returned error: %v", err)
			}
			var env struct {
				Errors  []EnvelopeError `json:"errors"`
				Results []healthRecord  `json:"results"`
			}
			if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
				t.Fatalf("envelope: %v\n%s", err, buf.String())
			}
			if len(env.Errors) != 0 {
				t.Errorf("errors = %+v, want none", env.Errors)
			}
			got := normalizeRecords(env.Results, dir)
			want := normalizeRecords(append([]healthRecord{}, state.records...), dir)
			if !equalJSON(t, got, want) {
				t.Errorf("records:\n got %+v\nwant %+v", got, want)
			}
		})

		t.Run(state.name+"/human", func(t *testing.T) {
			dir := pinHealthEnv(t)
			state.setup(t, dir)
			jsonOutputFlag = false

			c := &cobra.Command{Use: "health", RunE: runHealth}
			var runErr error
			out := captureStdout(t, func() { runErr = c.RunE(c, nil) })
			if runErr != nil {
				t.Fatalf("runHealth returned error: %v", runErr)
			}
			got := normalizeHuman(out, dir)
			want := normalizeHuman(strings.Join(state.human, "\n"), dir)
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Errorf("stdout:\n got %q\nwant %q", got, want)
			}
		})
	}
}

func equalJSON(t *testing.T, a, b any) bool {
	t.Helper()
	ja, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	jb, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Equal(ja, jb)
}

// TestOpenProjectWording pins the code and message queries get from
// openProject for each step the open sequence can stop at.
func TestOpenProjectWording(t *testing.T) {
	states := map[string]healthState{}
	for _, state := range healthStates() {
		states[state.name] = state
	}
	cases := []struct {
		state, code, message string
	}{
		{"not initialized", "not_initialized", "codegraph not initialized. Run 'codegraph init' first"},
		{"bad config", "config_load_failed", "failed to load config: invalid config: lsp_timeouts.request_seconds must be positive, got 0"},
		{"database missing", "database_missing", "database not found. Run 'codegraph build' first"},
		{"database open failure", "db_open_failed", "failed to open database: unable to open database file: permission denied"},
		{"not a database", "db_open_failed", "failed to open database: file is not a database"},
		{"incompatible format", "index_format_incompatible", (&db.IncompatibleFormatError{Found: 0}).Error()},
		{"healthy", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.state, func(t *testing.T) {
			dir := pinHealthEnv(t)
			states[tc.state].setup(t, dir)

			_, _, dbm, code, err := openProject()
			if dbm != nil {
				dbm.Close()
			}
			message := ""
			if err != nil {
				message = err.Error()
			}
			if code != tc.code || message != tc.message {
				t.Errorf("openProject = (%q, %q), want (%q, %q)", code, message, tc.code, tc.message)
			}
			if (dbm != nil) != (tc.code == "") {
				t.Errorf("manager returned = %v for code %q", dbm != nil, tc.code)
			}
		})
	}
}
