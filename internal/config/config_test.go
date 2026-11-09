package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

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
