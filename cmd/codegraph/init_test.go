package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tk-425/Codegraph/internal/config"
	"github.com/tk-425/Codegraph/internal/registry"
)

func TestInitCreatesMissingArtifacts(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gitignore := "generated/\n*.gen.go\n"
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(gitignore), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# Project instructions\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := runCLI(t, root, "init")
	if result.exit != 0 || len(result.stderr) != 0 {
		t.Fatalf("init: exit=%d stdout=%q stderr=%q", result.exit, result.stdout, result.stderr)
	}
	for _, name := range []string{"config.toml", ".cgignore"} {
		if !strings.Contains(string(result.stdout), "Created .codegraph/"+name) {
			t.Fatalf("missing creation message for %s: %q", name, result.stdout)
		}
	}
	if _, err := config.Load(root); err != nil {
		t.Fatalf("load generated config: %v", err)
	}
	for _, name := range []string{"graphs", "config.toml", ".cgignore"} {
		if _, err := os.Stat(filepath.Join(root, ".codegraph", name)); err != nil {
			t.Fatalf("missing setup artifact %s: %v", name, err)
		}
	}
	ignore, err := os.ReadFile(filepath.Join(root, ".codegraph", ".cgignore"))
	if err != nil || !strings.Contains(string(ignore), gitignore) {
		t.Fatalf("ignore seed: content=%q err=%v", ignore, err)
	}
	updatedGitignore, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil || !strings.HasPrefix(string(updatedGitignore), gitignore) || strings.Count(string(updatedGitignore), ".codegraph/\n") != 1 {
		t.Fatalf("gitignore update: content=%q err=%v", updatedGitignore, err)
	}
	agent, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil || strings.Count(string(agent), "<!-- codegraph:start -->") != 1 || strings.Count(string(agent), "<!-- codegraph:end -->") != 1 {
		t.Fatalf("agent note: content=%q err=%v", agent, err)
	}
	// runCLI sets HOME to root, so registration must remain inside this fixture.
	reg, err := registry.Load(filepath.Join(root, ".codegraph", registry.RegistryFile))
	if err != nil || len(reg.Projects) != 1 || reg.Projects[root] == nil {
		t.Fatalf("isolated registration: registry=%+v err=%v", reg, err)
	}
	if _, err := os.Stat(config.DefaultConfig().GetDatabasePath(root)); !os.IsNotExist(err) {
		t.Fatalf("init must not create a database: stat error=%v", err)
	}
}
