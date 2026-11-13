package lsp

import (
	"os"
	"path/filepath"
	"slices"
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
	resolved, err := ResolveServer(config.DefaultConfig(), root, "typescript")
	if err != nil {
		t.Fatal(err)
	}
	if !resolved.Native || resolved.Command != filepath.Join(root, "node_modules", ".bin", "tsc") {
		t.Fatalf("resolved = %#v", resolved)
	}
	if want := []string{"--lsp", "--stdio"}; !slices.Equal(resolved.Args, want) {
		t.Fatalf("args = %#v", resolved.Args)
	}
}

func TestResolveTypeScriptServerKeepsWrapperForOlderTypeScript(t *testing.T) {
	root := writeTypeScriptFixture(t, "5.6.3", true)
	resolved, err := ResolveServer(config.DefaultConfig(), root, "typescriptreact")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Native || resolved.Command != "typescript-language-server" {
		t.Fatalf("resolved = %#v", resolved)
	}
}

func TestResolveTypeScriptServerHonorsExplicitConfiguration(t *testing.T) {
	root := writeTypeScriptFixture(t, "7.0.2", true)
	cfg := config.DefaultConfig()
	cfg.LSP["typescript"] = config.LSPConfig{Command: "custom-ts-lsp", Args: []string{"--stdio", "--workspace"}}
	resolved, err := ResolveServer(cfg, root, "typescript")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Native || resolved.Command != "custom-ts-lsp" {
		t.Fatalf("resolved = %#v", resolved)
	}
	if !slices.Equal(resolved.Args, []string{"--stdio", "--workspace"}) {
		t.Fatalf("args = %#v", resolved.Args)
	}
}

func TestResolveServerPreservesConfiguredSelection(t *testing.T) {
	for _, language := range []string{"go", "typescript", "typescriptreact"} {
		t.Run(language, func(t *testing.T) {
			root := writeTypeScriptFixture(t, "7.0.2", true)
			cfg := config.DefaultConfig()
			configured := config.LSPConfig{Command: "custom-server", Args: []string{"--stdio", "--project", "custom path"}}
			cfg.LSP[language] = configured
			resolved, err := ResolveServer(cfg, root, language)
			if err != nil || resolved.Native || resolved.Command != configured.Command || !slices.Equal(resolved.Args, configured.Args) {
				t.Fatalf("resolved=%+v err=%v, want %+v", resolved, err, configured)
			}
			resolved.Args[0] = "changed"
			if cfg.LSP[language].Args[0] != "--stdio" {
				t.Fatal("selection aliases configured arguments")
			}
		})
	}
}

func TestResolveServerRequiresConfiguredLanguage(t *testing.T) {
	cfg := config.DefaultConfig()
	delete(cfg.LSP, "typescriptreact")
	for _, language := range []string{"unknown", "typescriptreact"} {
		if _, err := ResolveServer(cfg, t.TempDir(), language); err == nil {
			t.Fatalf("unconfigured %s resolved", language)
		}
	}
}

func TestResolveServerTypeScriptSelection(t *testing.T) {
	for _, test := range []struct {
		name, version string
		executable    bool
		native        bool
	}{
		{name: "native", version: "7.0.2", executable: true, native: true},
		{name: "older wrapper", version: "5.6.3", executable: true},
		{name: "missing executable", version: "7.0.2"},
		{name: "invalid version", version: "unknown", executable: true},
	} {
		for _, language := range []string{"typescript", "typescriptreact"} {
			t.Run(test.name+"/"+language, func(t *testing.T) {
				root := writeTypeScriptFixture(t, test.version, test.executable)
				resolved, err := ResolveServer(config.DefaultConfig(), root, language)
				command, args := "typescript-language-server", []string{"--stdio"}
				if test.native {
					command, args = filepath.Join(root, "node_modules", ".bin", "tsc"), []string{"--lsp", "--stdio"}
				}
				if err != nil || resolved.Native != test.native || resolved.Command != command || !slices.Equal(resolved.Args, args) {
					t.Fatalf("resolved=%+v err=%v, want %s %v native=%v", resolved, err, command, args, test.native)
				}
			})
		}
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
