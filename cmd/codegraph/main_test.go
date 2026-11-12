package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/tk-425/Codegraph/internal/config"
	"github.com/tk-425/Codegraph/internal/db"
)

// Re-execute the test binary in a fresh process, entering the real main().
// Exit directly so Go's test-status text cannot contaminate CLI stdout.
func TestCLIHelperProcess(t *testing.T) {
	if os.Getenv("CODEGRAPH_CLI_CHILD") != "1" {
		return
	}
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		os.Exit(2)
	}
	os.Args = append([]string{"codegraph"}, os.Args[separator+1:]...)
	main()
	os.Exit(0)
}

func cliCommand(t *testing.T, root string, args ...string) *exec.Cmd {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	childArgs := append([]string{"-test.run=^TestCLIHelperProcess$", "--"}, args...)
	command := exec.Command(binary, childArgs...)
	command.Dir = root
	command.Env = append(os.Environ(), "CODEGRAPH_CLI_CHILD=1", "HOME="+root)
	command.WaitDelay = time.Second
	return command
}

type cliResult struct {
	stdout, stderr []byte
	exit           int
}

func runCLI(t *testing.T, root string, args ...string) cliResult {
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
	case <-time.After(10 * time.Second):
		_ = command.Process.Kill()
		<-waited
		t.Fatalf("CLI watchdog expired for %v; stderr=%s", args, stderr.String())
	}
	return cliResult{stdout.Bytes(), stderr.Bytes(), command.ProcessState.ExitCode()}
}

type envelope struct {
	Command string            `json:"command"`
	Count   int               `json:"count"`
	Results []json.RawMessage `json:"results"`
	Errors  []struct {
		Code string `json:"code"`
	} `json:"errors"`
}

// requireOneEnvelope asserts stdout holds exactly one envelope with every key.
func requireOneEnvelope(t *testing.T, result cliResult) envelope {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(result.stdout))
	var raw map[string]json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		t.Fatalf("stdout is not one JSON envelope: %v; stdout=%q; stderr=%q; exit=%d", err, result.stdout, result.stderr, result.exit)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("extra stdout after envelope: value=%v, err=%v", extra, err)
	}
	for _, key := range []string{"command", "query", "count", "results", "errors"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("envelope missing %q", key)
		}
	}
	var decoded envelope
	if err := json.Unmarshal(result.stdout, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Count != len(decoded.Results) {
		t.Errorf("count=%d does not equal result records=%d", decoded.Count, len(decoded.Results))
	}
	return decoded
}

func requireErrorCode(t *testing.T, result cliResult, code string) envelope {
	t.Helper()
	if result.exit == 0 {
		t.Errorf("exit=0, want failure; stdout=%s", result.stdout)
	}
	decoded := requireOneEnvelope(t, result)
	if len(decoded.Errors) != 1 || decoded.Errors[0].Code != code {
		t.Errorf("errors=%+v, want one %q", decoded.Errors, code)
	}
	return decoded
}

func TestJSONRejectsCommandsWithoutJSONOutput(t *testing.T) {
	for _, args := range [][]string{
		{"init", "--json"},
		{"prune", "--json", "--force"},
		{"--json"},
		{"help", "search", "--json"},
		{"search", "--help", "--json"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			root := t.TempDir()
			requireErrorCode(t, runCLI(t, root, args...), "json_unsupported")
			if _, err := os.Stat(filepath.Join(root, ".codegraph")); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("rejected JSON invocation still changed the project (stat=%v)", err)
			}
		})
	}
}

func TestJSONReportsInvalidArgumentsAnywhereOnTheLine(t *testing.T) {
	for _, args := range [][]string{
		{"search", "--bogus", "Target", "--json"},
		{"search", "--json"},
		{"--json", "no-such-command"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			requireErrorCode(t, runCLI(t, t.TempDir(), args...), "invalid_arguments")
		})
	}
}

func TestHumanFlagErrorKeepsUsageOnStderr(t *testing.T) {
	result := runCLI(t, t.TempDir(), "search", "x", "--bogus")
	if result.exit == 0 {
		t.Error("unknown flag returned success")
	}
	if len(result.stdout) != 0 {
		t.Errorf("human-mode usage reached stdout: %q", result.stdout)
	}
	if !bytes.Contains(result.stderr, []byte("unknown flag: --bogus")) || !bytes.Contains(result.stderr, []byte("Usage:")) {
		t.Errorf("stderr lacks the error and usage: %q", result.stderr)
	}
}

func buildProtocolPeer(t *testing.T) string {
	t.Helper()
	fixture, err := filepath.Abs(filepath.Join("..", "..", "testdata", "characterization", "lsp"))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "scripted-lsp")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(ctx, "go", "build", "-o", binary, fixture).CombinedOutput(); err != nil {
		t.Fatalf("build protocol peer: %v\n%s", err, output)
	}
	return binary
}

func TestJSONBuildInterruptReportsCancelledAndReapsServer(t *testing.T) {
	root := t.TempDir()
	pidfile := filepath.Join(t.TempDir(), "peer.pid")
	cfg := config.DefaultConfig()
	cfg.LSP["go"] = config.LSPConfig{Command: buildProtocolPeer(t), Args: []string{"-mode", "block-after-initialize", "-pidfile", pidfile}}
	cfg.LSPTimeouts = config.LSPTimeoutsConfig{RequestSeconds: 30, ShutdownGracefulSeconds: 1, ShutdownForceSeconds: 1}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".codegraph", ".cgignore"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "source.go"), []byte("package fixture\n\nfunc Target() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	command := cliCommand(t, root, "build", "--json")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	t.Cleanup(func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			<-waited
		}
	})

	// The peer answers initialize, then stops reading: the build is now
	// blocked on a request that the 30-second budget would not end soon.
	var pid int
	deadline := time.Now().Add(10 * time.Second)
	for pid == 0 {
		if data, err := os.ReadFile(pidfile); err == nil && len(data) > 0 {
			pid, _ = strconv.Atoi(string(data))
		}
		if time.Now().After(deadline) {
			t.Fatalf("peer never started; stderr=%s", stderr.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	if err := command.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case <-waited:
	case <-time.After(10 * time.Second):
		t.Fatalf("T35: interrupted build did not exit; stderr=%s", stderr.String())
	}

	decoded := requireErrorCode(t, cliResult{stdout.Bytes(), stderr.Bytes(), command.ProcessState.ExitCode()}, "cancelled")
	if decoded.Command != "build" {
		t.Errorf("command=%q, want build", decoded.Command)
	}
	quality := decodeQuality(t, cliResult{stdout.Bytes(), stderr.Bytes(), command.ProcessState.ExitCode()})
	if quality.Metadata.Attempt == nil || quality.Metadata.Attempt.Outcome != "cancelled" {
		t.Fatalf("cancelled attempt missing from output: %s", stdout.String())
	}
	manager, err := db.OpenReadOnly(cfg.GetDatabasePath(root))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	attempt, err := manager.LatestAttempt()
	if err != nil {
		t.Fatal(err)
	}
	if attempt == nil || attempt.Outcome != "cancelled" {
		t.Fatalf("cancelled attempt not finalized: %+v", attempt)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Errorf("T35: language server %d survived the interrupted build (kill -0: %v)", pid, err)
	}
}

// SC1: one real CLI invocation connects extraction, publication and output.
func TestBuildSnapshotTracer(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.LSP["go"] = config.LSPConfig{Command: filepath.Join(root, "missing-lsp")}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".codegraph", ".cgignore"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "source.go"), []byte("package fixture\nfunc Target() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := runCLI(t, root, "build", "--json")
	requireOneEnvelope(t, result)
	if result.exit != 0 {
		t.Fatalf("build failed: %s %s", result.stdout, result.stderr)
	}
	var output struct {
		Metadata struct {
			Snapshot  *db.Snapshot `json:"snapshot"`
			Freshness string       `json:"freshness"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(result.stdout, &output); err != nil {
		t.Fatal(err)
	}
	m, err := db.OpenReadOnly(cfg.GetDatabasePath(root))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	stored, err := m.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if stored == nil || output.Metadata.Snapshot == nil || stored.ID == "" || stored.ID != output.Metadata.Snapshot.ID || output.Metadata.Freshness != "unchecked" {
		t.Fatalf("output=%+v stored=%+v", output.Metadata, stored)
	}
}

func TestJSONBrokenStdoutFailsWithoutRetry(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	reader.Close()
	defer writer.Close()

	command := cliCommand(t, t.TempDir(), "health", "--json")
	var stderr bytes.Buffer
	command.Stdout, command.Stderr = writer, &stderr
	if err := command.Run(); err == nil {
		t.Fatal("T36: write to a closed stdout reported success")
	}
	if bytes.Contains(stderr.Bytes(), []byte(`"command"`)) {
		t.Errorf("T36: envelope re-sent to stderr after stdout failed: %s", stderr.String())
	}
}
