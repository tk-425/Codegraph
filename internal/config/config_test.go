package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveIfAbsentCreatesConfigAndPreservesExistingContent(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig()
	cfg.Search.TimeoutSeconds = 17
	if err := SaveIfAbsent(root, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(root)
	if err != nil || loaded.Search.TimeoutSeconds != 17 {
		t.Fatalf("created config: loaded=%+v err=%v", loaded, err)
	}
	path := filepath.Join(root, DefaultConfigDir, "config.toml")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveIfAbsent(root, DefaultConfig()); !errors.Is(err, os.ErrExist) {
		t.Fatalf("existing config: got %v, want ErrExist", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("existing config changed: err=%v", err)
	}
	// Intentional saves retain their existing replacement behavior.
	if err := Save(root, DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	loaded, err = Load(root)
	if err != nil || loaded.Search.TimeoutSeconds != 30 {
		t.Fatalf("intentional save: loaded=%+v err=%v", loaded, err)
	}
}

func TestSaveIfAbsentPreservesExistingPaths(t *testing.T) {
	for _, kind := range []string{"directory", "symlink", "dangling_symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, DefaultConfigDir)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "config.toml")
			target := filepath.Join(root, "custom.toml")
			if kind == "directory" {
				if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatal(err)
				}
			} else {
				if kind == "symlink" {
					if err := os.WriteFile(target, []byte("# custom config\n"), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			}
			if err := SaveIfAbsent(root, DefaultConfig()); !errors.Is(err, os.ErrExist) {
				t.Fatalf("existing %s: got %v, want ErrExist", kind, err)
			}
			info, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "directory" {
				if !info.IsDir() {
					t.Fatal("directory replaced")
				}
			} else {
				link, err := os.Readlink(path)
				if err != nil || link != target {
					t.Fatalf("symlink changed: link=%q err=%v", link, err)
				}
				data, err := os.ReadFile(target)
				if kind == "symlink" && (err != nil || string(data) != "# custom config\n") {
					t.Fatalf("target changed: data=%q err=%v", data, err)
				}
				if kind == "dangling_symlink" && !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("dangling target created: err=%v", err)
				}
			}
		})
	}
}

func TestSaveIfAbsentReportsCreationFailure(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, DefaultConfigDir)
	if err := os.WriteFile(path, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveIfAbsent(root, DefaultConfig()); err == nil || errors.Is(err, os.ErrExist) {
		t.Fatalf("blocked config directory must fail, not report preserved: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "not a directory" {
		t.Fatalf("blocking file changed: data=%q err=%v", data, err)
	}
}

func TestDefaultConfigUsesAutomaticTypeScriptServer(t *testing.T) {
	cfg := DefaultConfig()
	for _, language := range []string{"typescript", "typescriptreact"} {
		got := cfg.LSP[language]
		if got.Command != "typescript-language-server" {
			t.Fatalf("%s command = %q", language, got.Command)
		}
		if len(got.Args) != 1 || got.Args[0] != "--stdio" {
			t.Fatalf("%s args = %#v", language, got.Args)
		}
	}
}

func writeTestConfig(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, DefaultConfigDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, DefaultConfigDir, "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestLoadDefaultsLSPTimeoutsWhenSectionAbsent(t *testing.T) {
	cfg, err := Load(writeTestConfig(t, "[search]\ntimeout_seconds = 30\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LSPTimeouts.Request() != 30*time.Second || cfg.LSPTimeouts.ShutdownGraceful() != 5*time.Second || cfg.LSPTimeouts.ShutdownForce() != 5*time.Second {
		t.Fatalf("timeouts = %+v, want 30s request and 5s/5s cleanup", cfg.LSPTimeouts)
	}
}

func TestLoadAppliesConfiguredLSPTimeouts(t *testing.T) {
	cfg, err := Load(writeTestConfig(t, "[lsp_timeouts]\nrequest_seconds = 2\nshutdown_graceful_seconds = 1\nshutdown_force_seconds = 3\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LSPTimeouts.Request() != 2*time.Second || cfg.LSPTimeouts.ShutdownGraceful() != time.Second || cfg.LSPTimeouts.ShutdownForce() != 3*time.Second {
		t.Fatalf("timeouts = %+v", cfg.LSPTimeouts)
	}
}

func TestLoadRejectsNonPositiveLSPTimeouts(t *testing.T) {
	for _, key := range []string{"request_seconds", "shutdown_graceful_seconds", "shutdown_force_seconds"} {
		for _, value := range []string{"0", "-1"} {
			t.Run(key+"="+value, func(t *testing.T) {
				if _, err := Load(writeTestConfig(t, "[lsp_timeouts]\n"+key+" = "+value+"\n")); err == nil {
					t.Fatalf("%s = %s was accepted; budgets must be positive", key, value)
				}
			})
		}
	}
}
