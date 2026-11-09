//go:build characterization

package lsp

import (
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

const characterizationRPCBudget = 200 * time.Millisecond
const characterizationSetupBudget = 5 * time.Second
const characterizationWatchdog = 2 * time.Second

func buildCharacterizationPeer(t *testing.T) string {
	t.Helper()
	fixture, err := filepath.Abs(filepath.Join("..", "..", "testdata", "characterization", "lsp"))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "scripted-lsp")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "build", "-o", binary, fixture)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build protocol peer: %v\n%s", err, output)
	}
	return binary
}

func startCharacterizationPeer(t *testing.T, binary, mode string) (*Client, *bool) {
	t.Helper()
	rootURI := (&url.URL{Scheme: "file", Path: t.TempDir()}).String()
	client, err := NewClient(binary, []string{"-mode", mode}, rootURI, "go")
	if err != nil {
		t.Fatal(err)
	}
	// Shutdown starts the client's Wait goroutine. Cleanup must not Wait twice.
	clientOwnsWait := new(bool)
	t.Cleanup(func() {
		_ = client.cmd.Process.Kill()
		_ = client.stdin.Close()
		_ = client.stdout.Close()
		if !*clientOwnsWait {
			_ = client.cmd.Wait()
			return
		}
		deadline := time.NewTimer(characterizationWatchdog)
		defer deadline.Stop()
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for {
			if err := client.cmd.Process.Signal(syscall.Signal(0)); errors.Is(err, os.ErrProcessDone) {
				return
			}
			select {
			case <-deadline.C:
				t.Error("harness cleanup: client did not reap its killed peer")
				return
			case <-tick.C:
			}
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), characterizationSetupBudget)
	defer cancel()
	if _, err := client.Initialize(ctx); err != nil {
		t.Fatalf("protocol peer did not initialize: %v", err)
	}
	return client, clientOwnsWait
}

func TestCharacterizationControlTransport(t *testing.T) {
	client, ownsWait := startCharacterizationPeer(t, buildCharacterizationPeer(t), "normal")
	ctx, cancel := context.WithTimeout(context.Background(), characterizationSetupBudget)
	defer cancel()
	if err := client.Call(ctx, "probe", nil, nil); err != nil {
		t.Fatalf("responsive peer request: %v", err)
	}
	if err := client.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	*ownsWait = true
	if err := client.cmd.Process.Signal(syscall.Signal(0)); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("responsive peer was not reaped: %v", err)
	}
}

func TestCharacterizationF06EOFFailsPendingRequest(t *testing.T) {
	client, _ := startCharacterizationPeer(t, buildCharacterizationPeer(t), "exit-on-probe")
	ctx, cancel := context.WithTimeout(context.Background(), characterizationRPCBudget)
	defer cancel()
	err := client.Call(ctx, "probe", nil, nil)
	if err == nil {
		t.Fatal("T24: peer exited but pending request reported success")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("T24: transport EOF was not propagated; request exhausted its caller deadline: %v", err)
	}
}

func TestCharacterizationF06CancellationInterruptsBlockedWrite(t *testing.T) {
	client, _ := startCharacterizationPeer(t, buildCharacterizationPeer(t), "block-after-initialize")
	ctx, cancel := context.WithTimeout(context.Background(), characterizationRPCBudget)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- client.Call(ctx, "probe", map[string]string{"payload": strings.Repeat("x", 2<<20)}, nil)
	}()
	watchdog := time.NewTimer(characterizationWatchdog)
	defer watchdog.Stop()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("T25: blocked request returned %v, want its caller deadline", err)
		}
	case <-watchdog.C:
		// Capture the blocked stack before closing the transport to recover.
		stack := make([]byte, 64<<10)
		n := runtime.Stack(stack, true)
		t.Logf("blocked request before harness recovery:\n%s", stack[:n])
		_ = client.stdin.Close()
		select {
		case <-done:
		case <-time.After(characterizationWatchdog):
			t.Fatal("harness recovery could not unblock the request")
		}
		t.Error("T25: caller deadline did not interrupt a blocked RPC write; harness closed stdin")
	}
}

func TestCharacterizationF06ShutdownTerminatesUnresponsivePeer(t *testing.T) {
	client, ownsWait := startCharacterizationPeer(t, buildCharacterizationPeer(t), "ignore-exit")
	ctx, cancel := context.WithTimeout(context.Background(), characterizationRPCBudget)
	defer cancel()
	if err := client.Shutdown(ctx); err != nil {
		t.Logf("shutdown error: %v", err)
	}
	*ownsWait = true
	if err := client.cmd.Process.Signal(syscall.Signal(0)); err == nil {
		t.Error("T26: shutdown returned with its owned peer still running; harness must kill/reap it")
	}
}
