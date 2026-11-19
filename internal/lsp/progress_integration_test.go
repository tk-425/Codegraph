//go:build integration

package lsp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// These opt-in checks use real servers and isolated, dependency-free fixtures.
// They establish query compatibility, not historical ContentModified causation
// or a universal readiness guarantee. A missing server is an unverified skip.
func TestProgressRealGopls(t *testing.T) {
	progressRealServer(t, "go", "gopls", "go.mod", "module progressfixture\n\ngo 1.25\n", "source.go", "package progressfixture\n\nfunc Target() {}\n")
}

func TestProgressRealRustAnalyzer(t *testing.T) {
	progressRealServer(t, "rust", "rust-analyzer", "Cargo.toml", "[package]\nname = \"progress_fixture\"\nversion = \"0.1.0\"\nedition = \"2021\"\n", "src/lib.rs", "pub fn Target() {}\n")
}

func progressRealServer(t *testing.T, language, command, manifest, manifestText, source, sourceText string) {
	t.Helper()
	server, err := exec.LookPath(command)
	if err != nil {
		t.Skipf("%s unavailable; real-server progress compatibility is INCONCLUSIVE: %v", command, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	versionArgs := []string{"--version"}
	if language == "go" {
		versionArgs = []string{"version"}
	}
	version, err := exec.CommandContext(ctx, server, versionArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("installed %s is unusable: %v: %s", command, err, version)
	}
	t.Logf("real server: %s", version)

	root := t.TempDir()
	for name, text := range map[string]string{manifest: manifestText, source: sourceText} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Never permit dependency downloads in a semantic compatibility smoke.
	t.Setenv("GOTOOLCHAIN", "local")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
	t.Setenv("CARGO_NET_OFFLINE", "true")
	t.Setenv("GOPATH", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	client, err := NewClient(server, nil, PathToURI(root), language)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Shutdown(context.Background()); err != nil {
			t.Errorf("server cleanup: %v", err)
		}
	})
	if _, err := client.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	uri := PathToURI(filepath.Join(root, source))
	if err := client.DidOpenTextDocument(uri, language, sourceText); err != nil {
		t.Fatal(err)
	}
	symbols, err := client.DocumentSymbols(ctx, uri)
	if err != nil {
		t.Fatalf("real %s document symbols: %v", command, err)
	}
	found := false
	for _, symbol := range symbols {
		if symbol.Name == "Target" {
			found = true
		}
	}
	if !found {
		t.Fatalf("real %s did not return fixture Target: %+v", command, symbols)
	}
	if err := client.DidCloseTextDocument(uri); err != nil {
		t.Fatal(err)
	}
	t.Log("fixture Target returned; no claim about progress silence or complete workspace readiness")
}
