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
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tk-425/Codegraph/internal/config"
	"github.com/tk-425/Codegraph/internal/db"
)

// runCLIIntegration is runCLI with a watchdog sized for real server startup.
func runCLIIntegration(t *testing.T, root string, timeout time.Duration, args ...string) cliResult {
	t.Helper()
	return runCLIIntegrationEnv(t, root, timeout, nil, args...)
}

// runCLIIntegrationEnv is runCLIIntegration with an explicit child
// environment; nil keeps cliCommand's default (inherited os.Environ). The
// build-context test needs to control ambient GOFLAGS.
func runCLIIntegrationEnv(t *testing.T, root string, timeout time.Duration, env []string, args ...string) cliResult {
	t.Helper()
	command := cliCommand(t, root, args...)
	if env != nil {
		command.Env = env
	}
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
	gopls := resolveIntegrationGopls(t)

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

// resolveIntegrationGopls locates the real gopls binary, or skips loudly:
// a skip here is an explicitly unverified capability, not a passing one.
func resolveIntegrationGopls(t *testing.T) string {
	t.Helper()
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
	return gopls
}

// TestIntegrationGoplsBuildContext drives the real CLI against real gopls
// with a persisted [lsp.go] build_flags entry: a custom-tagged file has
// package metadata only in the configured build context.
func TestIntegrationGoplsBuildContext(t *testing.T) {
	gopls := resolveIntegrationGopls(t)

	root := t.TempDir()
	write := func(rel string, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module fixture\n\ngo 1.25\n")
	write("base.go", "package fixture\n\nfunc Base() int { return 1 }\n")
	write("tagged.go", "//go:build fixturetag\n\npackage fixture\n\nfunc TaggedCaller() int { return Base() }\n")
	if err := os.MkdirAll(filepath.Join(root, ".codegraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(".codegraph", ".cgignore"), "")

	flaggedConfig := config.DefaultConfig()
	flaggedConfig.LSP["go"] = config.LSPConfig{Command: gopls, BuildFlags: []string{"-tags=fixturetag"}}
	plainConfig := config.DefaultConfig()
	plainConfig.LSP["go"] = config.LSPConfig{Command: gopls}

	// The child environment must not inherit an ambient GOFLAGS: the
	// effective context under test is the configured one.
	envWithoutGOFLAGS := func() []string {
		env := []string{}
		for _, entry := range os.Environ() {
			if strings.HasPrefix(entry, "GOFLAGS=") {
				continue
			}
			env = append(env, entry)
		}
		return append(env, "CODEGRAPH_CLI_CHILD=1", "HOME="+root)
	}
	buildSnapshot := func(result cliResult, changed bool) *db.Snapshot {
		t.Helper()
		output := requireOneEnvelope(t, result)
		if result.exit != 0 || output.Command != "build" || len(output.Errors) != 0 {
			t.Fatalf("build failed: exit=%d stdout=%s stderr=%s", result.exit, result.stdout, result.stderr)
		}
		var report struct {
			Metadata struct {
				Snapshot *db.Snapshot     `json:"snapshot"`
				Attempt  *db.BuildAttempt `json:"attempt"`
			} `json:"metadata"`
		}
		if err := json.Unmarshal(result.stdout, &report); err != nil {
			t.Fatal(err)
		}
		attempt := report.Metadata.Attempt
		if report.Metadata.Snapshot == nil || attempt == nil || attempt.Outcome != "succeeded" || attempt.Changed != changed {
			t.Fatalf("build did not publish the expected changed=%t attempt: %s", changed, result.stdout)
		}
		if changed {
			semanticCalls := false
			for _, phase := range attempt.Phases {
				if phase.Language == "go" && phase.Phase == "calls" && phase.Provider == "lsp" && (phase.Status == "complete" || phase.Status == "failed") {
					semanticCalls = true
				}
			}
			if !semanticCalls {
				t.Fatalf("build did not execute the Go semantic calls phase: %s", result.stdout)
			}
		}
		return report.Metadata.Snapshot
	}
	countRequestFailed := func(result cliResult) int {
		t.Helper()
		envelope := requireOneEnvelope(t, result)
		count := 0
		for _, raw := range envelope.Results {
			var record struct {
				Category string `json:"category"`
			}
			if err := json.Unmarshal(raw, &record); err != nil {
				t.Fatal(err)
			}
			if record.Category == "request_failed" {
				count++
			}
		}
		return count
	}
	requireTaggedCaller := func(env []string) {
		t.Helper()
		callers := runCLIIntegrationEnv(t, root, 60*time.Second, env, "callers", "Base", "--json")
		output := requireOneEnvelope(t, callers)
		if callers.exit != 0 || len(output.Errors) != 0 {
			t.Fatalf("callers exit=%d stdout=%s stderr=%s", callers.exit, callers.stdout, callers.stderr)
		}
		for _, raw := range output.Results {
			var record struct {
				Name     string   `json:"name"`
				Evidence []string `json:"evidence"`
			}
			if err := json.Unmarshal(raw, &record); err != nil {
				t.Fatal(err)
			}
			if record.Name == "TaggedCaller" && slices.Contains(record.Evidence, "lsp-call-hierarchy") {
				return
			}
		}
		t.Fatalf("callers Base does not include the custom-tagged caller with LSP evidence: %s", callers.stdout)
	}

	// SC2: configured tags, ambient GOFLAGS scrubbed. The tagged file has
	// package metadata, so no semantic request fails.
	if err := config.Save(root, flaggedConfig); err != nil {
		t.Fatal(err)
	}
	build := runCLIIntegrationEnv(t, root, 120*time.Second, envWithoutGOFLAGS(), "build", "--json")
	if build.exit != 0 {
		t.Fatalf("flagged build exit=%d stdout=%s stderr=%s", build.exit, build.stdout, build.stderr)
	}
	if failures := countRequestFailed(build); failures != 0 {
		t.Fatalf("flagged build reported %d request_failed records; the configured build context did not reach gopls: %s", failures, build.stdout)
	}

	initial := buildSnapshot(build, true)
	requireTaggedCaller(envWithoutGOFLAGS())

	// A control with the same context must reuse the existing publication.
	noop := runCLIIntegrationEnv(t, root, 120*time.Second, envWithoutGOFLAGS(), "build", "--json")
	if snapshot := buildSnapshot(noop, false); snapshot.ID != initial.ID {
		t.Fatalf("unchanged context replaced the snapshot: %s != %s", snapshot.ID, initial.ID)
	}

	// SC4: removing the flags re-extracts the language without --force; the
	// tagged file loses package metadata, so requests fail again.
	if err := config.Save(root, plainConfig); err != nil {
		t.Fatal(err)
	}
	unflagged := runCLIIntegrationEnv(t, root, 120*time.Second, envWithoutGOFLAGS(), "build", "--json")
	if unflagged.exit != 0 {
		t.Fatalf("unflagged build exit=%d stdout=%s stderr=%s", unflagged.exit, unflagged.stdout, unflagged.stderr)
	}
	if failures := countRequestFailed(unflagged); failures == 0 {
		t.Fatal("removing build_flags did not re-extract under the default context; no request_failed records")
	}

	removed := buildSnapshot(unflagged, true)
	if removed.ProviderDigest == initial.ProviderDigest || removed.ID == initial.ID {
		t.Fatal("removing build_flags retained the old provider identity or snapshot")
	}

	// SC4: restoring the flags invalidates again; failures clear.
	if err := config.Save(root, flaggedConfig); err != nil {
		t.Fatal(err)
	}
	restored := runCLIIntegrationEnv(t, root, 120*time.Second, envWithoutGOFLAGS(), "build", "--json")
	if restored.exit != 0 {
		t.Fatalf("restored build exit=%d stdout=%s stderr=%s", restored.exit, restored.stdout, restored.stderr)
	}
	if failures := countRequestFailed(restored); failures != 0 {
		t.Fatalf("restored build reported %d request_failed records: %s", failures, restored.stdout)
	}

	restoredSnapshot := buildSnapshot(restored, true)
	if restoredSnapshot.ID == removed.ID || restoredSnapshot.ProviderDigest != initial.ProviderDigest {
		t.Fatal("restoring build_flags did not publish the original provider context again")
	}
	requireTaggedCaller(envWithoutGOFLAGS())

	// SC3: an ambient GOFLAGS -tags entry conflicts with the configured one;
	// the configured tags win (go resolves repeated -tags last-wins), so the
	// forced rebuild still sees package metadata for the tagged file.
	precedence := runCLIIntegrationEnv(t, root, 120*time.Second, append(envWithoutGOFLAGS(), "GOFLAGS=-tags=other"), "build", "--force", "--json")
	if precedence.exit != 0 {
		t.Fatalf("precedence build exit=%d stdout=%s stderr=%s", precedence.exit, precedence.stdout, precedence.stderr)
	}
	if failures := countRequestFailed(precedence); failures != 0 {
		t.Fatalf("ambient GOFLAGS defeated the configured tags: %d request_failed records: %s", failures, precedence.stdout)
	}
	buildSnapshot(precedence, true)
	requireTaggedCaller(append(envWithoutGOFLAGS(), "GOFLAGS=-tags=other"))
}
