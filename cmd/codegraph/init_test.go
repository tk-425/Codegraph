package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tk-425/Codegraph/internal/config"
	"github.com/tk-425/Codegraph/internal/registry"
)

func TestInitPreservesExistingArtifacts(t *testing.T) {
	customConfig := "# Preserve comments and explicit commands.\n[lsp.go]\ncommand = \"custom-gopls\"\nargs = [\"--custom\"]\n[lsp_timeouts]\nrequest_seconds = 7\nshutdown_graceful_seconds = 2\nshutdown_force_seconds = 3\n[database]\npath = \".codegraph/custom.db\"\n"
	customIgnore := "# Keep indexing policy.\nprivate/\n!private/public.go\n"
	for _, test := range []struct {
		name   string
		config string
		ignore string
	}{
		{name: "neither"},
		{name: "config_only", config: customConfig},
		{name: "ignore_only", ignore: customIgnore},
		{name: "both", config: customConfig, ignore: customIgnore},
		{name: "invalid_config_is_not_repaired", config: "# Keep even invalid configuration.\n[", ignore: customIgnore},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, ".codegraph")
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			existing := map[string]string{
				".gitignore": ".codegraph/\ngenerated/\n",
				"AGENTS.md":  "# Keep project instructions.\n",
			}
			if test.config != "" {
				existing[".codegraph/config.toml"] = test.config
			}
			if test.ignore != "" {
				existing[".codegraph/.cgignore"] = test.ignore
			}
			for name, content := range existing {
				if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			first := runCLI(t, root, "init")
			if first.exit != 0 || len(first.stderr) != 0 {
				t.Fatalf("first init: exit=%d stdout=%q stderr=%q", first.exit, first.stdout, first.stderr)
			}
			for name, content := range map[string]string{"config.toml": test.config, ".cgignore": test.ignore} {
				verb := "Created"
				if content != "" {
					verb = "Preserved"
					data, err := os.ReadFile(filepath.Join(dir, name))
					if err != nil || string(data) != content {
						t.Fatalf("existing %s changed: data=%q err=%v", name, data, err)
					}
				}
				if !strings.Contains(string(first.stdout), verb+" .codegraph/"+name) {
					t.Fatalf("missing %s message for %s: %q", verb, name, first.stdout)
				}
			}
			before := make(map[string][]byte)
			for _, name := range []string{".codegraph/config.toml", ".codegraph/.cgignore", ".gitignore", "AGENTS.md"} {
				data, err := os.ReadFile(filepath.Join(root, name))
				if err != nil {
					t.Fatal(err)
				}
				before[name] = data
			}
			if string(before[".gitignore"]) != existing[".gitignore"] || !strings.HasPrefix(string(before["AGENTS.md"]), existing["AGENTS.md"]) {
				t.Fatal("existing gitignore or project instructions changed")
			}
			second := runCLI(t, root, "init")
			if second.exit != 0 || len(second.stderr) != 0 {
				t.Fatalf("repeat init: exit=%d stdout=%q stderr=%q", second.exit, second.stdout, second.stderr)
			}
			for _, name := range []string{"config.toml", ".cgignore"} {
				if !strings.Contains(string(second.stdout), "Preserved .codegraph/"+name) || strings.Contains(string(second.stdout), "Created .codegraph/"+name) {
					t.Fatalf("repeat init misreported %s: %q", name, second.stdout)
				}
			}
			for name, want := range before {
				data, err := os.ReadFile(filepath.Join(root, name))
				if err != nil || !bytes.Equal(data, want) {
					t.Fatalf("repeat init changed %s: err=%v", name, err)
				}
			}
			if strings.Count(string(before["AGENTS.md"]), "<!-- codegraph:start -->") != 1 || strings.Count(string(before["AGENTS.md"]), "<!-- codegraph:end -->") != 1 {
				t.Fatal("expected exactly one managed agent note")
			}
			entries, err := os.ReadDir(filepath.Join(dir, "graphs"))
			if err != nil || len(entries) != 0 {
				t.Fatalf("init populated graph storage: entries=%v err=%v", entries, err)
			}
			for _, name := range []string{"graphs/codegraph.db", "custom.db"} {
				if _, err := os.Lstat(filepath.Join(dir, name)); !os.IsNotExist(err) {
					t.Fatalf("init created database %s: %v", name, err)
				}
			}
		})
	}
}

func TestInitReportsCreationFailures(t *testing.T) {
	for _, kind := range []string{"blocked_directory", "unreadable_seed"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			blocked := ".codegraph"
			if kind == "unreadable_seed" {
				blocked = ".gitignore"
				if err := os.Mkdir(filepath.Join(root, blocked), 0o755); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(filepath.Join(root, blocked), []byte("keep this file"), 0o600); err != nil {
				t.Fatal(err)
			}
			result := runCLI(t, root, "init")
			if result.exit == 0 || !strings.Contains(string(result.stderr), "failed to create") {
				t.Fatalf("creation failure: exit=%d stdout=%q stderr=%q", result.exit, result.stdout, result.stderr)
			}
			if strings.Contains(string(result.stdout), "Preserved .codegraph/") || strings.Contains(string(result.stdout), "Done.") {
				t.Fatalf("failure reported as success: %q", result.stdout)
			}
			if kind == "blocked_directory" {
				data, err := os.ReadFile(filepath.Join(root, blocked))
				if err != nil || string(data) != "keep this file" {
					t.Fatalf("blocking file changed: data=%q err=%v", data, err)
				}
			} else if _, err := os.Lstat(filepath.Join(root, ".codegraph", ".cgignore")); !os.IsNotExist(err) {
				t.Fatalf("failed seed read left an ignore file: %v", err)
			}
		})
	}
}

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
