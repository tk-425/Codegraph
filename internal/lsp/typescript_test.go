package lsp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tk-425/Codegraph/internal/config"
)

func writeTypeScriptFixture(t *testing.T, version string, withExecutable bool) string {
	t.Helper()
	root := t.TempDir()
	pkg := filepath.Join(root, "node_modules", "typescript")
	if err := os.MkdirAll(filepath.Join(pkg, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "package.json"), []byte(`{"version":"`+version+`"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if withExecutable {
		bin := filepath.Join(root, "node_modules", ".bin")
		if err := os.MkdirAll(bin, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bin, "tsc"), []byte("#!/bin/sh\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestResolveTypeScriptServerUsesNativeServerForTypeScript7(t *testing.T) {
	root := writeTypeScriptFixture(t, "7.0.2", true)
	resolved, err := resolveTypeScriptServer(config.DefaultConfig(), root, "typescript")
	if err != nil {
		t.Fatal(err)
	}
	if !resolved.native || resolved.command != filepath.Join(root, "node_modules", ".bin", "tsc") {
		t.Fatalf("resolved = %#v", resolved)
	}
	if want := []string{"--lsp", "--stdio"}; len(resolved.args) != 2 || resolved.args[0] != want[0] || resolved.args[1] != want[1] {
		t.Fatalf("args = %#v", resolved.args)
	}
}

func TestResolveTypeScriptServerKeepsWrapperForOlderTypeScript(t *testing.T) {
	root := writeTypeScriptFixture(t, "5.6.3", true)
	resolved, err := resolveTypeScriptServer(config.DefaultConfig(), root, "typescriptreact")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.native || resolved.command != "typescript-language-server" {
		t.Fatalf("resolved = %#v", resolved)
	}
}

func TestResolveTypeScriptServerHonorsExplicitConfiguration(t *testing.T) {
	root := writeTypeScriptFixture(t, "7.0.2", true)
	cfg := config.DefaultConfig()
	cfg.LSP["typescript"] = config.LSPConfig{Command: "custom-ts-lsp", Args: []string{"--stdio", "--workspace"}}
	resolved, err := resolveTypeScriptServer(cfg, root, "typescript")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.native || resolved.command != "custom-ts-lsp" {
		t.Fatalf("resolved = %#v", resolved)
	}
	if len(resolved.args) != 2 || resolved.args[1] != "--workspace" {
		t.Fatalf("args = %#v", resolved.args)
	}
}

func TestParseTypeScriptVersion(t *testing.T) {
	got, err := parseTypeScriptVersion("v7.1.2")
	if err != nil {
		t.Fatal(err)
	}
	if got.Major != 7 || got.Minor != 1 || got.Patch != 2 {
		t.Fatalf("version = %#v", got)
	}
}
