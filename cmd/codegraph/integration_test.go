//go:build integration

// Opt-in real-server integration checks (fix-spec R11/T48). These never run
// in CI or the default suite: a skip here is an explicitly unverified
// capability, not a passing one. Run with:
//
//	go test -tags integration -count=1 ./cmd/codegraph
//
// gopls is located on PATH or via CODEGRAPH_INTEGRATION_GOPLS.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/tk-425/Codegraph/internal/config"
)

// runCLIIntegration is runCLI with a watchdog sized for real server startup.
func runCLIIntegration(t *testing.T, root string, timeout time.Duration, args ...string) cliResult {
	t.Helper()
	command := cliCommand(t, root, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	select {
	case <-waited:
	case <-time.After(timeout):
		_ = command.Process.Kill()
		<-waited
		t.Fatalf("integration CLI watchdog expired for %v; stderr=%s", args, stderr.String())
	}
	return cliResult{stdout.Bytes(), stderr.Bytes(), command.ProcessState.ExitCode()}
}

// TestIntegrationRealGopls drives the real CLI against a real gopls server:
// build, stored search and stored call queries over a small Go module.
func TestIntegrationRealGopls(t *testing.T) {
	gopls := os.Getenv("CODEGRAPH_INTEGRATION_GOPLS")
	if gopls == "" {
		resolved, err := exec.LookPath("gopls")
		if err != nil {
			t.Skipf("gopls not found; the real-server Go capability is NOT verified by this skip: %v", err)
		}
		gopls = resolved
	}
	if info, err := os.Stat(gopls); err != nil || info.IsDir() {
		t.Skipf("gopls %s unusable; the real-server Go capability is NOT verified by this skip: %v", gopls, err)
	}

	root := t.TempDir()
	write := func(rel string, content []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, rel), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", []byte("module fixture\n\ngo 1.25\n"))
	write("source.go", []byte(`package fixture

type Runner interface{ Run() }

type First struct{}

func (First) Run() {}

type Second struct{}

func (Second) Run() {}

func CallBoth(f First, s Second) {
	f.Run()
	s.Run()
}
`))
	if err := os.MkdirAll(filepath.Join(root, ".codegraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.LSP["go"] = config.LSPConfig{Command: gopls}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(".codegraph", ".cgignore"), nil)

	build := runCLIIntegration(t, root, 120*time.Second, "build", "--json")
	if build.exit != 0 {
		t.Fatalf("real-gopls build exit=%d stdout=%s stderr=%s", build.exit, build.stdout, build.stderr)
	}
	if envelope := requireOneEnvelope(t, build); len(envelope.Errors) != 0 {
		t.Fatalf("build reported errors: %s", build.stdout)
	}

	search := runCLIIntegration(t, root, 60*time.Second, "search", "Run", "--exact", "--json")
	if search.exit != 0 {
		t.Fatalf("search exit=%d stdout=%s stderr=%s", search.exit, search.stdout, search.stderr)
	}
	found := requireOneEnvelope(t, search)
	ids := map[string]bool{}
	for _, raw := range found.Results {
		var record struct {
			ID, Name, Kind, Language string
		}
		if err := json.Unmarshal(raw, &record); err != nil {
			t.Fatal(err)
		}
		if record.Name != "Run" || record.Kind != "method" || record.Language != "go" {
			t.Fatalf("unexpected search record: %+v", record)
		}
		if ids[record.ID] {
			t.Fatalf("duplicate symbol identity %q", record.ID)
		}
		ids[record.ID] = true
	}
	if len(ids) != 3 {
		t.Fatalf("exact search Run found %d distinct symbols (Runner, First, Second), want 3: %s", len(ids), search.stdout)
	}

	callers := runCLIIntegration(t, root, 60*time.Second, "callers", "Run", "--json")
	if callers.exit != 0 {
		t.Fatalf("callers exit=%d stdout=%s stderr=%s", callers.exit, callers.stdout, callers.stderr)
	}
	called := requireOneEnvelope(t, callers)
	sites := 0
	for _, raw := range called.Results {
		var record struct {
			Evidence []string
		}
		if err := json.Unmarshal(raw, &record); err != nil {
			t.Fatal(err)
		}
		if len(record.Evidence) == 0 {
			t.Fatalf("call record without evidence: %s", raw)
		}
		sites++
	}
	if sites < 2 {
		t.Fatalf("callers Run returned %d results, want the two dispatch sites at minimum: %s", sites, callers.stdout)
	}
}
