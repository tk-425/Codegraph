package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/tk-425/Codegraph/internal/registry"
)

func freshCmdNoArgs(t *testing.T, name string, run func(*cobra.Command, []string) error) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	c := &cobra.Command{Use: name, RunE: run}
	c.SetOut(buf)
	c.SetErr(buf)
	return c, buf
}

func assertQueryNull(t *testing.T, env map[string]json.RawMessage) {
	t.Helper()
	if string(env["query"]) != "null" {
		t.Errorf("query raw = %s, want null", string(env["query"]))
	}
}

func TestJSONDiagnostic_Health(t *testing.T) {
	_, _ = setupCodegraphProject(t)

	c, buf := freshCmdNoArgs(t, "health", runHealth)
	if err := c.RunE(c, nil); err != nil {
		t.Fatalf("runHealth returned error: %v", err)
	}

	env, count := decodeEnvelope(t, buf.Bytes())
	assertQueryNull(t, env)

	var cmdName string
	_ = json.Unmarshal(env["command"], &cmdName)
	if cmdName != "health" {
		t.Errorf("command = %q, want health", cmdName)
	}

	if count == 0 {
		t.Fatalf("expected at least one health record, got 0; envelope=%s", buf.String())
	}

	var recs []healthRecord
	_ = json.Unmarshal(env["results"], &recs)
	foundInit := false
	for _, r := range recs {
		if r.Category == "initialized" && r.OK {
			foundInit = true
		}
	}
	if !foundInit {
		t.Errorf("expected an initialized=true record, got %+v", recs)
	}
}

func TestJSONDiagnostic_Projects(t *testing.T) {
	_, _ = setupCodegraphProject(t)

	// Redirect HOME so registry.DefaultRegistryPath() points into the tempdir.
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	regDir := filepath.Join(homeDir, ".codegraph")
	if err := os.MkdirAll(regDir, 0o755); err != nil {
		t.Fatalf("mkdir registry dir: %v", err)
	}
	regPath := filepath.Join(regDir, "registry.json")

	reg := registry.New()
	reg.Projects["/nonexistent/path"] = &registry.Project{
		Name:          "ghostproj",
		Path:          "/nonexistent/path",
		InitializedAt: time.Unix(1, 0).UTC(),
		LastSeen:      time.Unix(2, 0).UTC(),
	}
	if err := reg.Save(regPath); err != nil {
		t.Fatalf("registry save: %v", err)
	}

	c, buf := freshCmdNoArgs(t, "projects", runProjects)
	if err := c.RunE(c, nil); err != nil {
		t.Fatalf("runProjects returned error: %v", err)
	}

	env, count := decodeEnvelope(t, buf.Bytes())
	assertQueryNull(t, env)

	if count != 1 {
		t.Fatalf("count = %d, want 1; envelope=%s", count, buf.String())
	}

	var recs []projectRecord
	_ = json.Unmarshal(env["results"], &recs)
	if recs[0].Name != "ghostproj" || recs[0].Status != "missing" {
		t.Errorf("record = %+v", recs[0])
	}
}

func TestJSONDiagnostic_Stats(t *testing.T) {
	_, _ = setupCodegraphProject(t)

	c, buf := freshCmdNoArgs(t, "stats", runStats)
	if err := c.RunE(c, nil); err != nil {
		t.Fatalf("runStats returned error: %v", err)
	}

	env, count := decodeEnvelope(t, buf.Bytes())
	assertQueryNull(t, env)

	if count != 1 {
		t.Fatalf("count = %d, want 1 (stats returns one aggregated record); envelope=%s", count, buf.String())
	}

	var recs []statsRecord
	if err := json.Unmarshal(env["results"], &recs); err != nil {
		t.Fatalf("results unmarshal: %v", err)
	}
	// Database path should be populated even when DB is freshly initialized.
	if recs[0].DatabasePath == "" {
		t.Errorf("database_path should be populated, got empty; record=%+v", recs[0])
	}
}
