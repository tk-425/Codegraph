package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tk-425/Codegraph/internal/registry"
)

func TestProjectsDeduplicatePreviewAndApply(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	oldJSON, oldDeduplicate, oldApply := jsonOutputFlag, projectsDeduplicateFlag, projectsApplyFlag
	t.Cleanup(func() {
		jsonOutputFlag, projectsDeduplicateFlag, projectsApplyFlag = oldJSON, oldDeduplicate, oldApply
	})
	project := filepath.Join(home, "project")
	alias := filepath.Join(home, "alias")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(project, alias); err != nil {
		t.Fatal(err)
	}
	path, err := registry.DefaultRegistryPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Update(t.Context(), path, func(r *registry.Registry) error {
		for _, key := range []string{project, alias} {
			r.Projects[key] = &registry.Project{Name: "project", Path: key, InitializedAt: time.Unix(1, 0).UTC(), LastSeen: time.Unix(2, 0).UTC()}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	jsonOutputFlag, projectsDeduplicateFlag, projectsApplyFlag = true, true, false
	cmd, buf := freshCmdNoArgs(t, "projects", runProjects)
	cmd.SetContext(t.Context())
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatal(err)
	}
	env, count := decodeEnvelope(t, buf.Bytes())
	assertQueryNull(t, env)
	if count != 1 {
		t.Fatalf("preview count=%d", count)
	}
	current, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(current, original) {
		t.Fatalf("preview changed registry: %v", err)
	}
	buf.Reset()
	projectsApplyFlag = true
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatal(err)
	}
	env, count = decodeEnvelope(t, buf.Bytes())
	var groups []registry.DuplicateGroup
	if err := json.Unmarshal(env["results"], &groups); err != nil {
		t.Fatal(err)
	}
	if count != 1 || groups[0].Backup == "" {
		t.Fatalf("apply groups=%+v", groups)
	}
	backup, err := os.ReadFile(groups[0].Backup)
	if err != nil || !bytes.Equal(backup, original) {
		t.Fatalf("backup does not preserve original: %v", err)
	}
	buf.Reset()
	jsonOutputFlag = false
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "No duplicate projects") {
		t.Fatalf("human no-op: %s", buf.String())
	}
}

func TestProjectsApplyRequiresDeduplicate(t *testing.T) {
	oldJSON, oldDeduplicate, oldApply := jsonOutputFlag, projectsDeduplicateFlag, projectsApplyFlag
	t.Cleanup(func() {
		jsonOutputFlag, projectsDeduplicateFlag, projectsApplyFlag = oldJSON, oldDeduplicate, oldApply
	})
	jsonOutputFlag, projectsDeduplicateFlag, projectsApplyFlag = true, false, true
	cmd, buf := freshCmdNoArgs(t, "projects", runProjects)
	if err := cmd.RunE(cmd, nil); err == nil {
		t.Fatal("--apply without --deduplicate succeeded")
	}
	env, count := decodeEnvelope(t, buf.Bytes())
	if count != 0 || !bytes.Contains(env["errors"], []byte("invalid_arguments")) {
		t.Fatalf("wrong flag error: %s", buf.String())
	}
}
