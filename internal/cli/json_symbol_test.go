package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/tk-425/Codegraph/internal/db"
)

// setupCodegraphProject creates a temp project with .codegraph/ and an
// initialized SQLite database, changes the working directory to it, and
// returns the open db.Manager. It registers cleanups to close the manager
// and reset jsonOutputFlag.
func setupCodegraphProject(t *testing.T) (string, *db.Manager) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".codegraph", "graphs"), 0o755); err != nil {
		t.Fatalf("mkdir .codegraph/graphs: %v", err)
	}
	t.Chdir(dir)

	dbPath := filepath.Join(dir, ".codegraph", "graphs", "codegraph.db")
	m, err := db.NewManager(dbPath)
	if err != nil {
		t.Fatalf("db.NewManager: %v", err)
	}
	if err := m.Initialize(); err != nil {
		m.Close()
		t.Fatalf("db.Initialize: %v", err)
	}
	t.Cleanup(func() { m.Close() })

	jsonOutputFlag = true
	t.Cleanup(func() { jsonOutputFlag = false })

	return dir, m
}

// freshCmd returns a cobra.Command wired with the same RunE and arg requirements
// as the production command. Tests invoke runX directly so that the global
// rootCmd flag/parse state stays clean.
func freshCmd(t *testing.T, name string, run func(*cobra.Command, []string) error) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	c := &cobra.Command{Use: name, Args: cobra.ExactArgs(1), RunE: run}
	c.SetOut(buf)
	c.SetErr(buf)
	return c, buf
}

func decodeEnvelope(t *testing.T, raw []byte) (map[string]json.RawMessage, int) {
	t.Helper()
	var env map[string]json.RawMessage
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("envelope is not valid JSON: %v\nraw=%s", err, string(raw))
	}
	for _, k := range []string{"command", "query", "count", "results", "errors"} {
		if _, ok := env[k]; !ok {
			t.Fatalf("envelope missing key %q: %s", k, string(raw))
		}
	}
	var count int
	if err := json.Unmarshal(env["count"], &count); err != nil {
		t.Fatalf("count unmarshal: %v", err)
	}
	if len(env["results"]) == 0 || env["results"][0] != '[' {
		t.Fatalf("results should be array, got: %s", string(env["results"]))
	}
	return env, count
}

func seedSymbol(t *testing.T, m *db.Manager, s db.Symbol) {
	t.Helper()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Unix(0, 0)
	}
	if err := m.InsertSymbol(&s); err != nil {
		t.Fatalf("InsertSymbol(%s): %v", s.Name, err)
	}
}

func TestJSONSymbol_Search(t *testing.T) {
	_, m := setupCodegraphProject(t)
	seedSymbol(t, m, db.Symbol{
		ID: "src/auth.go#authenticate", Name: "authenticate", Kind: "function",
		File: "src/auth.go", Line: 42, Language: "go", Signature: "func authenticate(u User) bool",
	})

	c, buf := freshCmd(t, "search", runSearch)
	if err := c.RunE(c, []string{"authenticate"}); err != nil {
		t.Fatalf("runSearch returned error: %v", err)
	}

	env, count := decodeEnvelope(t, buf.Bytes())

	var cmdName string
	_ = json.Unmarshal(env["command"], &cmdName)
	if cmdName != "search" {
		t.Errorf("command = %q, want search", cmdName)
	}
	if string(env["query"]) != `"authenticate"` {
		t.Errorf("query raw = %s, want \"authenticate\"", string(env["query"]))
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}

	var recs []searchRecord
	if err := json.Unmarshal(env["results"], &recs); err != nil {
		t.Fatalf("results unmarshal: %v", err)
	}
	if recs[0].Name != "authenticate" || recs[0].Kind != "function" {
		t.Errorf("record = %+v", recs[0])
	}
}

func TestJSONSymbol_Signature(t *testing.T) {
	_, m := setupCodegraphProject(t)
	seedSymbol(t, m, db.Symbol{
		ID: "src/auth.go#authenticate", Name: "authenticate", Kind: "function",
		File: "src/auth.go", Line: 42, Language: "go", Signature: "func authenticate(u User) bool",
	})

	c, buf := freshCmd(t, "signature", runSignature)
	if err := c.RunE(c, []string{"authenticate"}); err != nil {
		t.Fatalf("runSignature returned error: %v", err)
	}

	env, count := decodeEnvelope(t, buf.Bytes())
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	var recs []signatureRecord
	_ = json.Unmarshal(env["results"], &recs)
	if recs[0].Signature != "func authenticate(u User) bool" {
		t.Errorf("signature = %q", recs[0].Signature)
	}
}

func TestSignatureStoredContentAvailability(t *testing.T) {
	for _, tc := range []struct {
		name, kind, signature, docs, signatureStatus, docsStatus string
	}{
		{"full", "function", " func target(\n value int,\n) bool ", " Stored documentation. ", "available", "available"},
		{"partial", "method", " : double ", "", "available", "unavailable"},
		{"absent", "variable", "", "", "unavailable", "unavailable"},
		{"whitespace", "function", " \n\t", " \n\t", "unavailable", "unavailable"},
		{"docs-only", "function", "", "Known documentation", "unavailable", "available"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, manager := setupCodegraphProject(t)
			oldLang := signatureLangFlag
			signatureLangFlag = "go"
			t.Cleanup(func() { signatureLangFlag = oldLang })
			path := filepath.Join(root, "source.go")
			if err := os.WriteFile(path, []byte("LIVE_SOURCE_NOT_STORED\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			seedSymbol(t, manager, db.Symbol{
				ID: "target-id", Name: "target", Kind: tc.kind, File: path, Line: 1,
				Language: "go", Source: "lsp", Signature: tc.signature, Documentation: tc.docs,
			})
			command, output := freshCmd(t, "signature", runSignature)
			if err := command.RunE(command, []string{"target"}); err != nil {
				t.Fatal(err)
			}
			env, count := decodeEnvelope(t, output.Bytes())
			var records []signatureRecord
			if err := json.Unmarshal(env["results"], &records); err != nil {
				t.Fatal(err)
			}
			if count != 1 || len(records) != 1 {
				t.Fatalf("expected one stored result: %s", output.String())
			}
			record := records[0]
			if record.Signature != strings.TrimSpace(tc.signature) || record.Documentation != strings.TrimSpace(tc.docs) || record.SignatureStatus != tc.signatureStatus || record.DocumentationStatus != tc.docsStatus {
				t.Fatalf("stored content/availability changed: %+v", record)
			}
			if record.ID != "target-id" || record.Source != "lsp" || record.Origin != "stored" || record.File != "source.go" || record.Line != 1 || record.Kind != tc.kind {
				t.Fatalf("identity/provenance/location changed: %+v", record)
			}
			var raw []map[string]json.RawMessage
			if err := json.Unmarshal(env["results"], &raw); err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"signature", "documentation", "signature_status", "documentation_status"} {
				if _, ok := raw[0][field]; !ok {
					t.Errorf("missing explicit field %s", field)
				}
			}
			jsonOutputFlag = false
			command, output = freshCmd(t, "signature", runSignature)
			command.SetErr(&bytes.Buffer{})
			if err := command.RunE(command, []string{"target"}); err != nil {
				t.Fatal(err)
			}
			for _, field := range []struct{ label, status, content string }{
				{"Signature information", tc.signatureStatus, strings.TrimSpace(tc.signature)},
				{"Documentation", tc.docsStatus, strings.TrimSpace(tc.docs)},
			} {
				want := field.content
				if field.status == "unavailable" {
					want = "unavailable"
				}
				if !strings.Contains(output.String(), field.label+": "+want) {
					t.Errorf("human output missing stored availability/content %q: %s", field.label, output.String())
				}
			}
			if strings.Contains(output.String(), "LIVE_SOURCE_NOT_STORED") {
				t.Fatal("live source substituted for stored content")
			}
		})
	}
}

func TestSignatureLanguageAndKindFilters(t *testing.T) {
	root, manager := setupCodegraphProject(t)
	oldLang := signatureLangFlag
	signatureLangFlag = "go,ocaml"
	t.Cleanup(func() { signatureLangFlag = oldLang })
	for _, symbol := range []db.Symbol{
		{ID: "go-method", Name: "target", Kind: "method", Language: "go"},
		{ID: "ocaml-binding", Name: "target", Kind: "variable", Language: "ocaml"},
		{ID: "go-class", Name: "target", Kind: "class", Language: "go"},
		{ID: "java-function", Name: "target", Kind: "function", Language: "java"},
	} {
		symbol.File = filepath.Join(root, symbol.ID)
		seedSymbol(t, manager, symbol)
	}
	command, output := freshCmd(t, "signature", runSignature)
	if err := command.RunE(command, []string{"target"}); err != nil {
		t.Fatal(err)
	}
	env, count := decodeEnvelope(t, output.Bytes())
	var records []signatureRecord
	if err := json.Unmarshal(env["results"], &records); err != nil {
		t.Fatal(err)
	}
	if count != 2 || len(records) != 2 {
		t.Fatalf("filter results: %s", output.String())
	}
	for _, record := range records {
		if record.ID != "go-method" && record.ID != "ocaml-binding" {
			t.Errorf("ineligible result: %+v", record)
		}
	}
}

func TestJSONSymbol_Callers(t *testing.T) {
	_, m := setupCodegraphProject(t)
	caller := db.Symbol{
		ID: "src/handler.go#handleLogin", Name: "handleLogin", Kind: "function",
		File: "src/handler.go", Line: 10, Language: "go",
	}
	callee := db.Symbol{
		ID: "src/auth.go#authenticate", Name: "authenticate", Kind: "function",
		File: "src/auth.go", Line: 42, Language: "go",
	}
	seedSymbol(t, m, caller)
	seedSymbol(t, m, callee)
	if err := m.InsertCall(&db.Call{
		CallerID: caller.ID, CalleeID: callee.ID,
		File: "src/handler.go", Line: 15, Column: 4,
	}); err != nil {
		t.Fatalf("InsertCall: %v", err)
	}

	c, buf := freshCmd(t, "callers", runCallers)
	if err := c.RunE(c, []string{"authenticate"}); err != nil {
		t.Fatalf("runCallers returned error: %v", err)
	}

	env, count := decodeEnvelope(t, buf.Bytes())
	if count != 1 {
		t.Fatalf("count = %d, want 1, env=%s", count, buf.String())
	}
	var recs []callerRecord
	_ = json.Unmarshal(env["results"], &recs)
	if recs[0].Name != "handleLogin" || recs[0].Line != 15 {
		t.Errorf("record = %+v", recs[0])
	}
}

func TestJSONSymbol_Callees(t *testing.T) {
	_, m := setupCodegraphProject(t)
	caller := db.Symbol{
		ID: "src/handler.go#handleLogin", Name: "handleLogin", Kind: "function",
		File: "src/handler.go", Line: 10, Language: "go",
	}
	callee := db.Symbol{
		ID: "src/auth.go#authenticate", Name: "authenticate", Kind: "function",
		File: "src/auth.go", Line: 42, Language: "go",
	}
	seedSymbol(t, m, caller)
	seedSymbol(t, m, callee)
	if err := m.InsertCall(&db.Call{
		CallerID: caller.ID, CalleeID: callee.ID,
		File: "src/handler.go", Line: 15, Column: 4,
	}); err != nil {
		t.Fatalf("InsertCall: %v", err)
	}

	c, buf := freshCmd(t, "callees", runCallees)
	if err := c.RunE(c, []string{"handleLogin"}); err != nil {
		t.Fatalf("runCallees returned error: %v", err)
	}

	env, count := decodeEnvelope(t, buf.Bytes())
	if count != 1 {
		t.Fatalf("count = %d, want 1, env=%s", count, buf.String())
	}
	var recs []calleeRecord
	_ = json.Unmarshal(env["results"], &recs)
	if recs[0].Name != "authenticate" {
		t.Errorf("record = %+v", recs[0])
	}
}

func TestJSONSymbol_Implementations(t *testing.T) {
	_, m := setupCodegraphProject(t)
	iface := db.Symbol{
		ID: "src/io.go#Reader", Name: "Reader", Kind: "interface",
		File: "src/io.go", Line: 5, Language: "go",
	}
	impl := db.Symbol{
		ID: "src/file.go#FileReader", Name: "FileReader", Kind: "struct",
		File: "src/file.go", Line: 10, Language: "go",
	}
	seedSymbol(t, m, iface)
	seedSymbol(t, m, impl)
	if err := m.InsertTypeHierarchy(&db.TypeHierarchy{
		ChildID: impl.ID, ParentID: iface.ID, Relationship: "implements",
	}); err != nil {
		t.Fatalf("InsertTypeHierarchy: %v", err)
	}

	c, buf := freshCmd(t, "implementations", runImplementations)
	if err := c.RunE(c, []string{"Reader"}); err != nil {
		t.Fatalf("runImplementations returned error: %v", err)
	}

	env, count := decodeEnvelope(t, buf.Bytes())
	if count != 1 {
		t.Fatalf("count = %d, want 1, env=%s", count, buf.String())
	}
	var recs []implementationRecord
	_ = json.Unmarshal(env["results"], &recs)
	if recs[0].Name != "FileReader" {
		t.Errorf("record = %+v", recs[0])
	}
}

func TestJSONSymbol_Types(t *testing.T) {
	setupCodegraphProject(t)

	c, buf := freshCmd(t, "types", runTypes)
	err := c.RunE(c, []string{"Reader"})
	if err == nil {
		t.Fatalf("runTypes should return non-nil error while stubbed; envelope=%s", buf.String())
	}

	env, count := decodeEnvelope(t, buf.Bytes())
	if count != 0 {
		t.Errorf("count = %d, want 0 (types stub)", count)
	}
	if string(env["query"]) != `"Reader"` {
		t.Errorf("query raw = %s", string(env["query"]))
	}
	var cmdName string
	_ = json.Unmarshal(env["command"], &cmdName)
	if cmdName != "types" {
		t.Errorf("command = %q, want types", cmdName)
	}

	var errs []EnvelopeError
	if uerr := json.Unmarshal(env["errors"], &errs); uerr != nil {
		t.Fatalf("errors unmarshal: %v", uerr)
	}
	if len(errs) != 1 || errs[0].Code != "not_implemented" {
		t.Errorf("errors = %+v, want one entry with code=not_implemented", errs)
	}
}
