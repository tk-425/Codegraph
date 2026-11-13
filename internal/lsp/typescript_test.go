package lsp

import (
	"errors"
	"fmt"
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

func TestResolveJavaServerIsolatesProjectWorkspaces(t *testing.T) {
	parent := t.TempDir()
	cfg := config.DefaultConfig()
	workspaces := make(map[string]bool)
	for _, project := range []string{"first", "second"} {
		// Equal basenames must not collapse distinct projects; URI-sensitive
		// characters must survive the manager's root conversion.
		root := filepath.Join(parent, project, "java project #😀")
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, input := range []string{root, root + "/.", projectRootFromURI(PathToURI(root))} {
			server, err := ResolveServer(cfg, input, "java")
			want := filepath.Join(root, config.DefaultConfigDir, "jdtls-workspace")
			if err != nil || server.Command != "jdtls" || server.Native || !slices.Equal(server.Args, []string{"-data", want}) || !filepath.IsAbs(server.Args[1]) {
				t.Fatalf("resolve %q = %+v, err=%v, want jdtls -data %q", input, server, err, want)
			}
			workspaces[server.Args[1]] = true
			if _, err := os.Stat(filepath.Join(root, config.DefaultConfigDir)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("resolution created project state: %v", err)
			}
		}
	}
	if len(workspaces) != 2 || len(cfg.LSP["java"].Args) != 0 {
		t.Fatalf("workspace isolation/config preservation: paths=%v config=%+v", workspaces, cfg.LSP["java"])
	}
	// Relative roots still produce absolute server arguments.
	t.Chdir(parent)
	server, err := ResolveServer(cfg, filepath.Join("first", "java project #😀"), "java")
	if err != nil || !slices.Equal(server.Args, []string{"-data", filepath.Join(parent, "first", "java project #😀", config.DefaultConfigDir, "jdtls-workspace")}) {
		t.Fatalf("relative project root: server=%+v err=%v", server, err)
	}
}

func TestResolveJavaServerPreservesExplicitConfiguration(t *testing.T) {
	for _, configured := range []config.LSPConfig{
		{Command: "jdtls", Args: []string{"-data", "/tmp/jdtls-workspace"}},
		{Command: "jdtls", Args: []string{"-data", "custom workspace"}},
		{Command: "jdtls", Args: []string{"--custom-flag"}},
		{Command: "custom-java"},
		{Command: "custom-java", Args: []string{"-data", "custom workspace"}},
	} {
		t.Run(configured.Command+fmt.Sprint(configured.Args), func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.LSP["java"] = configured
			before := slices.Clone(configured.Args)
			server, err := ResolveServer(cfg, t.TempDir(), "java")
			if err != nil || server.Native || server.Command != configured.Command || !slices.Equal(server.Args, configured.Args) {
				t.Fatalf("override changed: server=%+v err=%v, want %+v", server, err, configured)
			}
			if len(server.Args) > 0 {
				server.Args[0] = "changed result"
				if !slices.Equal(cfg.LSP["java"].Args, before) {
					t.Fatal("resolution aliased configured args")
				}
			}
		})
	}
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
