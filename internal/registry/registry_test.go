//go:build darwin || linux

package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"
)

// Re-execute the test binary in a fresh process so concurrency tests contend
// on a real inter-process lock, not goroutines inside one process.
func TestRegistryHelperProcess(t *testing.T) {
	if os.Getenv("CODEGRAPH_REGISTRY_CHILD") != "1" {
		return
	}
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		os.Exit(2)
	}
	args := os.Args[separator+1:]
	if len(args) < 2 {
		os.Exit(2)
	}
	mode, path := args[0], args[1]
	var err error
	switch mode {
	case "add":
		// add <path> <key> <count>: count sequential registrations
		key := args[2]
		count, convErr := strconv.Atoi(args[3])
		if convErr != nil {
			os.Exit(2)
		}
		for i := range count {
			n := i
			err = Update(context.Background(), path, func(r *Registry) error {
				r.Add(fmt.Sprintf("/projects/%s-%d", key, n), key)
				return nil
			})
			if err != nil {
				break
			}
		}
	case "hold":
		// hold <path> <marker>: acquire the lock, signal, block until killed
		marker := args[2]
		err = Update(context.Background(), path, func(*Registry) error {
			if writeErr := os.WriteFile(marker, []byte("locked"), 0o600); writeErr != nil {
				return writeErr
			}
			<-make(chan struct{})
			return nil
		})
	default:
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func registryChild(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	childArgs := append([]string{"-test.run=^TestRegistryHelperProcess$", "--"}, args...)
	command := exec.Command(binary, childArgs...)
	command.Env = append(os.Environ(), "CODEGRAPH_REGISTRY_CHILD=1")
	command.WaitDelay = 5 * time.Second
	return command
}

func startChild(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	command := registryChild(t, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	return command
}

func waitChild(t *testing.T, command *exec.Cmd, timeout time.Duration) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("child %v: %v", command.Args, err)
		}
	case <-time.After(timeout):
		_ = command.Process.Kill()
		<-done
		t.Fatalf("child watchdog expired for %v", command.Args)
	}
}

// SC1: concurrent processes retain every completed registration, with the
// existing InitializedAt/LastSeen meanings preserved.
func TestUpdateConcurrentProcessesRetainRegistrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	const workers, updates = 8, 5

	commands := make([]*exec.Cmd, 0, workers)
	for w := range workers {
		commands = append(commands, startChild(t, "add", path,
			fmt.Sprintf("worker%d", w), strconv.Itoa(updates)))
	}
	for _, command := range commands {
		waitChild(t, command, 30*time.Second)
	}

	reg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Projects) != workers*updates {
		t.Fatalf("got %d projects, want %d", len(reg.Projects), workers*updates)
	}
	for w := range workers {
		for i := range updates {
			key := fmt.Sprintf("/projects/worker%d-%d", w, i)
			if _, ok := reg.Projects[key]; !ok {
				t.Fatalf("lost registration %q", key)
			}
		}
	}
	if reg.Version != RegistryVersion {
		t.Fatalf("version=%q, want %q", reg.Version, RegistryVersion)
	}
}

func TestUpdatePreservesTimestampSemantics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	add := func(name string) {
		t.Helper()
		if err := Update(context.Background(), path, func(r *Registry) error {
			r.Add("/proj", name)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}

	add("first")
	reg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	first := reg.Projects["/proj"]
	if first.InitializedAt.IsZero() || first.LastSeen.IsZero() {
		t.Fatalf("timestamps not set: %+v", first)
	}
	time.Sleep(2 * time.Millisecond)

	add("second")
	reg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	second := reg.Projects["/proj"]
	if !second.InitializedAt.Equal(first.InitializedAt) {
		t.Fatal("InitializedAt changed on re-registration")
	}
	if second.LastSeen.Before(second.InitializedAt) {
		t.Fatal("LastSeen did not advance past InitializedAt")
	}
	if second.Name != "second" {
		t.Fatalf("name=%q, want %q", second.Name, "second")
	}
}

// SC2: readers see complete JSON during publication; failures keep the
// previous publication and clean unpublished temporaries.
func TestUpdateLeavesCompleteJSONAndNoTemporaries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")
	if err := Update(context.Background(), path, func(r *Registry) error {
		r.Add("/projects/seed", "seed")
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	const updates = 50
	command := startChild(t, "add", path, "writer", strconv.Itoa(updates))
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()

	reads := 0
	readOnce := func() {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reader saw broken publication: %v", err)
		}
		var reg Registry
		if err := json.Unmarshal(data, &reg); err != nil {
			t.Fatalf("reader saw partial JSON: %v", err)
		}
		reads++
	}
poll:
	for {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("writer child: %v", err)
			}
			break poll
		default:
			readOnce()
			time.Sleep(200 * time.Microsecond)
		}
	}
	readOnce()
	if reads == 0 {
		t.Fatal("no reads performed")
	}

	reg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Projects) != updates+1 {
		t.Fatalf("got %d projects, want %d", len(reg.Projects), updates+1)
	}
	temps, err := filepath.Glob(filepath.Join(dir, ".registry-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(temps) != 0 {
		t.Fatalf("unpublished temporaries left behind: %v", temps)
	}
	if _, err := os.Stat(path + ".lock"); err != nil {
		t.Fatalf("lock file should stay in place: %v", err)
	}
}

func TestUpdateFailureLeavesPreviousPublication(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")
	if err := Update(context.Background(), path, func(r *Registry) error {
		r.Add("/projects/keep", "keep")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	sentinel := errors.New("update rejected")
	if err := Update(context.Background(), path, func(r *Registry) error {
		r.Add("/projects/transient", "transient")
		return sentinel
	}); !errors.Is(err, sentinel) {
		t.Fatalf("err=%v, want callback sentinel", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("failed update changed the publication")
	}
	temps, err := filepath.Glob(filepath.Join(dir, ".registry-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(temps) != 0 {
		t.Fatalf("unpublished temporaries left behind: %v", temps)
	}
}

func TestUpdateMalformedRegistryIsNeverRepaired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Update(context.Background(), path, func(r *Registry) error {
		r.Add("/projects/na", "na")
		return nil
	}); err == nil {
		t.Fatal("malformed registry accepted for update")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{not json" {
		t.Fatalf("malformed bytes were rewritten: %q", data)
	}
}

// SC3: contention and cancellation are bounded; a killed owner does not
// permanently block writers.
func TestUpdateContentionCancellationIsBounded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")
	marker := filepath.Join(dir, "locked")

	holder := startChild(t, "hold", path, marker)
	t.Cleanup(func() {
		_ = holder.Process.Kill()
		_ = holder.Wait()
	})
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("holder never acquired the lock")
		}
		time.Sleep(5 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := Update(ctx, path, func(r *Registry) error {
		r.Add("/projects/after", "after")
		return nil
	})
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("contended update blocked %v despite cancellation", elapsed)
	}
	if err == nil {
		t.Fatal("contended update succeeded without the lock")
	}
	reg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Projects["/projects/after"]; ok {
		t.Fatal("cancelled update was published")
	}
}

func TestUpdateKilledOwnerDoesNotBlockWriters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")
	marker := filepath.Join(dir, "locked")
	if err := Update(context.Background(), path, func(r *Registry) error {
		r.Add("/projects/keep", "keep")
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	holder := startChild(t, "hold", path, marker)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = holder.Process.Kill()
			t.Fatal("holder never acquired the lock")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := holder.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := holder.Wait(); err == nil {
		t.Fatal("holder expected to die from SIGKILL")
	}

	start := time.Now()
	if err := Update(context.Background(), path, func(r *Registry) error {
		r.Add("/projects/after", "after")
		return nil
	}); err != nil {
		t.Fatalf("update after owner death failed: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("update after owner death took %v", elapsed)
	}
	reg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Projects["/projects/keep"]; !ok {
		t.Fatal("interrupted update lost the previous publication")
	}
	if _, ok := reg.Projects["/projects/after"]; !ok {
		t.Fatal("new registration missing after owner death")
	}
}
