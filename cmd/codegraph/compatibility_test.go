package main

import (
	"archive/tar"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/tk-425/Codegraph/internal/config"
)

// Compile the actual released source, offline, into a test-owned fixture. Tests
// never fetch tags, check out branches, use the installed binary, or touch the
// user's index. CI fetches release history explicitly and requires this check.
func version090Binary(t *testing.T) string {
	t.Helper()
	repository, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	check := exec.Command("git", "rev-parse", "--verify", "refs/tags/v0.9.0^{commit}")
	check.Dir = repository
	if output, err := check.CombinedOutput(); err != nil {
		if os.Getenv("CODEGRAPH_REQUIRE_COMPATIBILITY") == "1" {
			t.Fatalf("required local v0.9.0 source unavailable: %v %s", err, output)
		}
		t.Skipf("local v0.9.0 source unavailable; real downgrade/write compatibility not verified: %v", err)
	}
	archive := exec.Command("git", "archive", "--format=tar", "v0.9.0")
	archive.Dir = repository
	data, err := archive.Output()
	if err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	reader := tar.NewReader(bytes.NewReader(data))
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		path := filepath.Join(source, filepath.FromSlash(header.Name))
		if !strings.HasPrefix(path, source+string(filepath.Separator)) {
			t.Fatalf("invalid archived path %q", header.Name)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		_, copyErr := io.Copy(file, reader)
		closeErr := file.Close()
		if copyErr != nil {
			t.Fatal(copyErr)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	}
	binary := filepath.Join(t.TempDir(), "codegraph-v0.9.0")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/codegraph")
	build.Dir = source
	build.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "CGO_ENABLED=1")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("offline v0.9.0 build: %v\n%s", err, output)
	}
	return binary
}

func runOldCLI(t *testing.T, binary, root string, args ...string) cliResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, args...)
	command.Dir = root
	command.Env = append(os.Environ(), "HOME="+root)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if ctx.Err() != nil {
		t.Fatalf("v0.9.0 command exceeded watchdog: %v", args)
	}
	if err != nil && command.ProcessState == nil {
		t.Fatal(err)
	}
	return cliResult{stdout.Bytes(), stderr.Bytes(), command.ProcessState.ExitCode()}
}

func requireCompatibleSuccess(t *testing.T, result cliResult) qualityOutput {
	t.Helper()
	output := decodeQuality(t, result)
	if result.exit != 0 {
		t.Fatalf("compatibility command failed: stdout=%s stderr=%s", result.stdout, result.stderr)
	}
	return output
}

func requireFormatTwo(t *testing.T, path string) {
	t.Helper()
	database, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var version int
	if err := database.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 2 {
		t.Fatalf("format=%d, want unchanged format 2", version)
	}
}

func TestCancelledBuildDoesNotReportConcurrentlyInvalidatedSnapshot(t *testing.T) {
	root, cfg := qualityProject(t)
	initial := requireCompatibleSuccess(t, runCLI(t, root, "build", "--json"))
	if initial.Metadata.Snapshot == nil {
		t.Fatal("fixture needs a published starting snapshot")
	}
	pidfile := filepath.Join(t.TempDir(), "peer.pid")
	cfg.LSP["go"] = config.LSPConfig{Command: buildProtocolPeer(t), Args: []string{"-mode", "block-after-initialize", "-pidfile", pidfile}}
	cfg.LSPTimeouts.RequestSeconds = 30
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	command := cliCommand(t, root, "build", "--json")
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	t.Cleanup(func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			<-done
		}
	})
	pid := 0
	deadline := time.Now().Add(10 * time.Second)
	for pid == 0 {
		if data, err := os.ReadFile(pidfile); err == nil {
			pid, _ = strconv.Atoi(string(data))
		}
		if time.Now().After(deadline) {
			t.Fatal("peer did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Provider launch follows the build's starting publication/revision reads.
	// An unaware connection now invalidates that summary while extraction waits.
	older, err := sql.Open("sqlite3", cfg.GetDatabasePath(root))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := older.Exec(`UPDATE provider_state SET digest = 'concurrent-older-writer' WHERE language = 'go'`); err != nil {
		older.Close()
		t.Fatal(err)
	}
	if err := older.Close(); err != nil {
		t.Fatal(err)
	}
	if err := command.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("cancelled build did not exit")
	}
	result := cliResult{stdout.Bytes(), stderr.Bytes(), command.ProcessState.ExitCode()}
	requireErrorCode(t, result, "cancelled")
	output := decodeQuality(t, result)
	if output.Metadata.Snapshot != nil || output.Metadata.Attempt == nil || output.Metadata.Attempt.Outcome != "cancelled" {
		t.Fatalf("failure reported stale starting publication: %+v", output.Metadata)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Errorf("peer survived cancelled build: %v", err)
	}
}

func TestVersion090UpgradeDowngradeWriteCompatibility(t *testing.T) {
	old := version090Binary(t)
	root, cfg := qualityProject(t)
	for language := range cfg.LSP {
		cfg.LSP[language] = config.LSPConfig{Command: filepath.Join(root, "missing-lsp")}
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "source.py"), []byte("class Base:\n    pass\n\nclass Derived(Base):\n    pass\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	databasePath := cfg.GetDatabasePath(root)
	requireCompatibleSuccess(t, runOldCLI(t, old, root, "build", "--json"))
	requireFormatTwo(t, databasePath)
	before, err := os.ReadFile(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"search", "Target", "--json"}, {"signature", "Target", "--json"},
		{"callers", "Target", "--json"}, {"callees", "Caller", "--json"},
		{"implementations", "Base", "--json"}, {"stats", "--json"}, {"health", "--json"},
	} {
		output := requireCompatibleSuccess(t, runCLI(t, root, args...))
		if output.Metadata.Snapshot != nil || output.Metadata.Attempt != nil || output.Metadata.Freshness != "unchecked" {
			t.Fatalf("invented original-index metadata for %v: %+v", args, output.Metadata)
		}
		if args[0] == "callers" || args[0] == "callees" || args[0] == "implementations" {
			if len(output.Results) != 1 {
				t.Fatalf("lost original relationship for %v: %+v", args, output.Results)
			}
			var record struct{ Evidence []string }
			if err := json.Unmarshal(output.Results[0], &record); err != nil {
				t.Fatal(err)
			}
			if len(record.Evidence) != 1 || record.Evidence[0] != "unknown" {
				t.Fatalf("fabricated historical evidence: %+v", record)
			}
		}
	}
	after, err := os.ReadFile(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("upgrading queries migrated/changed original database")
	}
	published := requireCompatibleSuccess(t, runCLI(t, root, "build", "--json")) // deliberately no --force
	if published.Metadata.Snapshot == nil || published.Metadata.Attempt == nil || published.Metadata.Attempt.Outcome != "succeeded" {
		t.Fatal("ordinary upgrading build did not establish quality")
	}
	requireFormatTwo(t, databasePath)
	for _, args := range [][]string{{"search", "Target", "--json"}, {"callers", "Target", "--json"}, {"callees", "Caller", "--json"}, {"implementations", "Base", "--json"}, {"stats", "--json"}} {
		requireCompatibleSuccess(t, runOldCLI(t, old, root, args...))
	}
	// A real older publisher fails after its DELETEs; invalidation must roll back
	// with those DELETEs, preserving the newer publication and original graph.
	database, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TRIGGER reject_compatibility_symbol BEFORE INSERT ON symbols WHEN NEW.name = 'Rejected' BEGIN SELECT RAISE(ABORT, 'compatibility fixture failure'); END`); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "source.go"), []byte("package fixture\nfunc Rejected() {}\nfunc Caller() { Rejected() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	requireErrorCode(t, runOldCLI(t, old, root, "build", "--force", "--json"), "index_failed")
	stillPublished := requireCompatibleSuccess(t, runCLI(t, root, "stats", "--json"))
	if stillPublished.Metadata.Snapshot == nil || stillPublished.Metadata.Snapshot.ID != published.Metadata.Snapshot.ID {
		t.Fatal("failed old publisher invalidated/replaced committed quality")
	}
	if err := os.WriteFile(filepath.Join(root, "source.go"), []byte("package fixture\nfunc Replacement() {}\nfunc Caller() { Replacement() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	requireCompatibleSuccess(t, runOldCLI(t, old, root, "build", "--json"))
	requireFormatTwo(t, databasePath)
	changed := requireCompatibleSuccess(t, runCLI(t, root, "callers", "Replacement", "--json"))
	if changed.Metadata.Snapshot != nil || changed.Metadata.Attempt != nil || len(changed.Results) != 1 {
		t.Fatalf("old changed graph inherited newer quality: %+v", changed)
	}
	var legacyCall struct{ Evidence []string }
	if err := json.Unmarshal(changed.Results[0], &legacyCall); err != nil {
		t.Fatal(err)
	}
	if len(legacyCall.Evidence) != 1 || legacyCall.Evidence[0] != "unknown" {
		t.Fatalf("older writer fabricated new evidence: %+v", legacyCall)
	}
	restored := requireCompatibleSuccess(t, runCLI(t, root, "build", "--json")) // ordinary build recovers metadata
	if restored.Metadata.Snapshot == nil || restored.Metadata.Snapshot.ID == published.Metadata.Snapshot.ID {
		t.Fatal("new ordinary build did not recover invalidated quality")
	}
	current := requireCompatibleSuccess(t, runCLI(t, root, "callers", "Replacement", "--json"))
	var currentCall struct{ Evidence []string }
	if err := json.Unmarshal(current.Results[0], &currentCall); err != nil {
		t.Fatal(err)
	}
	if len(currentCall.Evidence) != 1 || currentCall.Evidence[0] != "tree-sitter-call" {
		t.Fatalf("new build did not re-establish provenance: %+v", currentCall)
	}
	noop := requireCompatibleSuccess(t, runCLI(t, root, "build", "--json"))
	if noop.Metadata.Snapshot.ID != restored.Metadata.Snapshot.ID {
		t.Fatal("new no-op lost original timestamp/identity contract")
	}
	// v0.9.0 republishes provider_state on a no-op. Conservative invalidation is
	// intentional: a writer unaware of quality cannot revalidate the report.
	requireCompatibleSuccess(t, runOldCLI(t, old, root, "build", "--json"))
	unknown := requireCompatibleSuccess(t, runCLI(t, root, "stats", "--json"))
	if unknown.Metadata.Snapshot != nil || unknown.Metadata.Attempt != nil {
		t.Fatal("old no-op left potentially stale quality trusted")
	}
	final := requireCompatibleSuccess(t, runCLI(t, root, "build", "--json"))
	if final.Metadata.Snapshot == nil {
		t.Fatal("new ordinary build failed to restore old-no-op invalidation")
	}
	requireFormatTwo(t, databasePath)
}
