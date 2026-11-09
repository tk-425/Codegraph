package lsp

import (
	"context"
	"errors"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Budgets injected for transport failure cases; not performance claims.
const lifecycleBudget = 200 * time.Millisecond
const lifecycleSetup = 5 * time.Second

var (
	lifecyclePeerOnce sync.Once
	lifecyclePeerPath string
	lifecyclePeerErr  error
)

// lifecyclePeer builds the scripted protocol peer once per test binary.
func lifecyclePeer(t *testing.T) string {
	t.Helper()
	lifecyclePeerOnce.Do(func() {
		dir, err := filepath.Abs(filepath.Join("..", "..", "testdata", "characterization", "lsp"))
		if err != nil {
			lifecyclePeerErr = err
			return
		}
		lifecyclePeerPath = filepath.Join(lifecycleBuildDir, "scripted-lsp")
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if output, err := exec.CommandContext(ctx, "go", "build", "-o", lifecyclePeerPath, dir).CombinedOutput(); err != nil {
			lifecyclePeerErr = errors.New(string(output))
		}
	})
	if lifecyclePeerErr != nil {
		t.Fatalf("build protocol peer: %v", lifecyclePeerErr)
	}
	return lifecyclePeerPath
}

func startLifecyclePeer(t *testing.T, mode string) *Client {
	t.Helper()
	rootURI := (&url.URL{Scheme: "file", Path: t.TempDir()}).String()
	client, err := NewClient(lifecyclePeer(t), []string{"-mode", mode}, rootURI, "go")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil && !strings.Contains(err.Error(), "was killed") {
			t.Errorf("close: %v", err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), lifecycleSetup)
	defer cancel()
	if _, err := client.Initialize(ctx); err != nil {
		t.Fatalf("protocol peer did not initialize: %v", err)
	}
	return client
}

func TestMalformedFramingFailsPendingRequest(t *testing.T) {
	for _, mode := range []string{"bad-length-on-probe", "no-length-on-probe", "oversized-on-probe", "bad-json-on-probe"} {
		t.Run(mode, func(t *testing.T) {
			client := startLifecyclePeer(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), lifecycleSetup)
			defer cancel()
			err := client.Call(ctx, "probe", nil, nil)
			if !errors.Is(err, ErrTransportClosed) {
				t.Fatalf("malformed frame: err = %v, want ErrTransportClosed before the caller deadline", err)
			}
		})
	}
}

func TestRequestBudgetBoundsCallWithoutCallerDeadline(t *testing.T) {
	client := startLifecyclePeer(t, "block-after-initialize")
	client.requestTimeout = lifecycleBudget
	done := make(chan error, 1)
	go func() { done <- client.Call(context.Background(), "probe", nil, nil) }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want the request budget to expire", err)
		}
	case <-time.After(lifecycleSetup):
		t.Fatal("request without a caller deadline was not bounded by the request budget")
	}
}

func TestAbandonedWriteClosesTransportForLaterCalls(t *testing.T) {
	client := startLifecyclePeer(t, "block-after-initialize")
	ctx, cancel := context.WithTimeout(context.Background(), lifecycleBudget)
	defer cancel()
	if err := client.Call(ctx, "probe", map[string]string{"payload": strings.Repeat("x", 2<<20)}, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked write: err = %v, want caller deadline", err)
	}
	started := time.Now()
	if err := client.Call(context.Background(), "probe", nil, nil); !errors.Is(err, ErrTransportClosed) {
		t.Fatalf("later call: err = %v, want ErrTransportClosed", err)
	}
	if elapsed := time.Since(started); elapsed > lifecycleBudget {
		t.Errorf("later call took %s on a closed transport; want an immediate failure", elapsed)
	}
	select {
	case <-client.exited:
	case <-time.After(lifecycleSetup):
		t.Error("broken transport did not kill and reap its server")
	}
}
