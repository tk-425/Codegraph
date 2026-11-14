package main

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tk-425/Codegraph/internal/registry"
)

// runCLIIn runs one CLI child to completion with an explicit home and working
// directory, mirroring runCLI for projects that share one registry.
func runCLIIn(t *testing.T, home, dir string, args ...string) cliResult {
	t.Helper()
	command := cliCommandIn(t, home, dir, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("child %v: %v; stderr=%q", args, err, stderr.String())
		}
	case <-time.After(15 * time.Second):
		_ = command.Process.Kill()
		<-done
		t.Fatalf("watchdog expired for %v", args)
	}
	return cliResult{stdout.Bytes(), stderr.Bytes(), command.ProcessState.ExitCode()}
}

// interactiveCLI is a CLI child held at a prompt, with a stdin pipe to answer
// it once the test has arranged the concurrent state it wants.
type interactiveCLI struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	stderr  *bytes.Buffer
	seen    strings.Builder
}

func startInteractiveCLI(t *testing.T, home, dir string, args ...string) *interactiveCLI {
	t.Helper()
	command := cliCommandIn(t, home, dir, args...)
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	child := &interactiveCLI{command: command, stdin: stdin, stdout: bufio.NewReader(stdout), stderr: &stderr}
	t.Cleanup(func() {
		_ = stdin.Close()
		if command.Process != nil && command.ProcessState == nil {
			_ = command.Process.Kill()
		}
	})
	return child
}

// waitPrompt blocks until want appears in the child's stdout. The prompt has
// no trailing newline, so reads must return on any bytes, not per line.
func (c *interactiveCLI) waitPrompt(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	chunk := make([]byte, 4096)
	for {
		if strings.Contains(c.seen.String(), want) {
			return
		}
		n, err := c.stdout.Read(chunk)
		if n > 0 {
			c.seen.Write(chunk[:n])
		}
		if err != nil {
			t.Fatalf("stdout closed before %q: seen=%q err=%v", want, c.seen.String(), err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %q: seen=%q", want, c.seen.String())
		}
	}
}

// answer sends the prompt response, drains remaining stdout to EOF, waits
// for the child, and returns its combined stdout.
func (c *interactiveCLI) answer(t *testing.T, response string) string {
	t.Helper()
	if _, err := io.WriteString(c.stdin, response); err != nil {
		t.Fatal(err)
	}
	if err := c.stdin.Close(); err != nil {
		t.Fatal(err)
	}
	chunk := make([]byte, 4096)
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for {
			n, err := c.stdout.Read(chunk)
			if n > 0 {
				c.seen.Write(chunk[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	select {
	case <-drained:
	case <-time.After(15 * time.Second):
		_ = c.command.Process.Kill()
		t.Fatalf("watchdog expired after answering; stdout=%q stderr=%q", c.seen.String(), c.stderr.String())
	}
	if err := c.command.Wait(); err != nil {
		t.Fatalf("child wait: %v; stdout=%q stderr=%q", err, c.seen.String(), c.stderr.String())
	}
	if code := c.command.ProcessState.ExitCode(); code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, c.seen.String(), c.stderr.String())
	}
	return c.seen.String()
}

func seedRegistration(t *testing.T, regPath, path, name string, lastSeen time.Time) {
	t.Helper()
	if err := registry.Update(context.Background(), regPath, func(reg *registry.Registry) error {
		reg.Projects[path] = &registry.Project{
			Name:          name,
			Path:          path,
			InitializedAt: lastSeen,
			LastSeen:      lastSeen,
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func loadRegistry(t *testing.T, regPath string) *registry.Registry {
	t.Helper()
	reg, err := registry.Load(regPath)
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

// SC4: prune removes only approved candidates that are still missing;
// registrations refreshed by a concurrent init, paths that exist again, and
// unrelated entries all survive.
func TestPruneRevalidatesApprovedCandidates(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	regPath := filepath.Join(home, ".codegraph", registry.RegistryFile)
	seedTime := time.Unix(1000000, 0).UTC()

	kept := filepath.Join(root, "kept")
	if err := os.Mkdir(kept, 0o755); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(root, "gone")
	refreshed := filepath.Join(root, "refreshed")
	returned := filepath.Join(root, "returned")
	seedRegistration(t, regPath, gone, "gone", seedTime)           // still missing: removed
	seedRegistration(t, regPath, refreshed, "refreshed", seedTime) // re-registered during the prompt: kept
	seedRegistration(t, regPath, returned, "returned", seedTime)   // directory exists again during the prompt: kept
	seedRegistration(t, regPath, kept, "kept", seedTime)           // not a candidate: kept untouched

	prune := startInteractiveCLI(t, home, home, "prune")
	prune.waitPrompt(t, "Remove these from registry?")

	// While prune waits at its prompt, re-register one candidate and bring
	// another one's directory back.
	if err := os.Mkdir(refreshed, 0o755); err != nil {
		t.Fatal(err)
	}
	runCLIIn(t, home, refreshed, "init")
	if err := os.Mkdir(returned, 0o755); err != nil {
		t.Fatal(err)
	}

	stdout := prune.answer(t, "y\n")
	if !strings.Contains(stdout, "Removed 1 projects from registry") {
		t.Fatalf("removed count wrong: %q", stdout)
	}

	reg := loadRegistry(t, regPath)
	if len(reg.Projects) != 3 {
		t.Fatalf("got %d projects, want 3: %+v", len(reg.Projects), reg.Projects)
	}
	if _, ok := reg.Projects[gone]; ok {
		t.Fatal("still-missing approved candidate survived")
	}
	refreshedEntry := reg.Projects[refreshed]
	if refreshedEntry == nil || refreshedEntry.LastSeen.Equal(seedTime) {
		t.Fatalf("refreshed registration was pruned or not refreshed: %+v", refreshedEntry)
	}
	returnedEntry := reg.Projects[returned]
	if returnedEntry == nil || !returnedEntry.LastSeen.Equal(seedTime) {
		t.Fatalf("returned-path registration was pruned: %+v", returnedEntry)
	}
	untouched := reg.Projects[kept]
	if untouched == nil || !untouched.LastSeen.Equal(seedTime) {
		t.Fatalf("unrelated registration changed: %+v", untouched)
	}

	result := runCLI(t, home, "projects", "--json")
	output := requireOneEnvelope(t, result)
	if result.exit != 0 || len(result.stderr) != 0 || output.Count != 3 || len(output.Errors) != 0 {
		t.Fatalf("projects after prune: exit=%d count=%d errors=%v stderr=%q",
			result.exit, output.Count, output.Errors, result.stderr)
	}
}

// SC5: force prune keeps its existing unattended behavior, and declining the
// prompt leaves the registry untouched.
func TestPruneForceAndDeclinedPrompt(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	regPath := filepath.Join(home, ".codegraph", registry.RegistryFile)
	seedTime := time.Unix(1000000, 0).UTC()
	missing := filepath.Join(root, "missing")
	existing := filepath.Join(root, "existing")
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	seedRegistration(t, regPath, missing, "missing", seedTime)
	seedRegistration(t, regPath, existing, "existing", seedTime)

	forced := runCLIIn(t, home, home, "prune", "--force")
	if forced.exit != 0 || !strings.Contains(string(forced.stdout), "Removed 1 projects from registry") {
		t.Fatalf("force prune: exit=%d stdout=%q stderr=%q", forced.exit, forced.stdout, forced.stderr)
	}
	reg := loadRegistry(t, regPath)
	if len(reg.Projects) != 1 || reg.Projects[existing] == nil {
		t.Fatalf("force prune result: %+v", reg.Projects)
	}

	seedRegistration(t, regPath, missing, "missing", seedTime)
	declined := startInteractiveCLI(t, home, home, "prune")
	declined.waitPrompt(t, "Remove these from registry?")
	stdout := declined.answer(t, "n\n")
	if !strings.Contains(stdout, "Aborted") || strings.Contains(stdout, "Removed") {
		t.Fatalf("declined prompt output: %q", stdout)
	}
	reg = loadRegistry(t, regPath)
	if len(reg.Projects) != 2 || reg.Projects[missing] == nil {
		t.Fatalf("declined prune changed registry: %+v", reg.Projects)
	}
}
