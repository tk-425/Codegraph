package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func contentModifiedPeer(t *testing.T, method string, replies []any) *Client {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		method: map[string]any{"__sequence__": replies},
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(t.TempDir(), "responses.json")
	if err := os.WriteFile(fixture, data, 0o644); err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(lifecyclePeer(t), []string{"-responses", fixture}, "", "go")
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
		t.Fatal(err)
	}
	return client
}

func rpcFailure(code int) any {
	return map[string]any{"__error__": map[string]any{"code": code, "message": "scripted failure"}}
}

func TestContentModifiedRetries(t *testing.T) {
	for _, test := range []struct {
		name     string
		method   string
		replies  []any
		attempts int64
		code     int
	}{
		{"recover", "textDocument/references", []any{rpcFailure(-32801), []string{"recovered"}}, 2, 0},
		{"symbols", "textDocument/documentSymbol", []any{rpcFailure(-32801), []string{"recovered"}}, 2, 0},
		{"prepare-calls", "textDocument/prepareCallHierarchy", []any{rpcFailure(-32801), []string{"recovered"}}, 2, 0},
		{"incoming-calls", "callHierarchy/incomingCalls", []any{rpcFailure(-32801), []string{"recovered"}}, 2, 0},
		{"supertypes", "typeHierarchy/supertypes", []any{rpcFailure(-32801), []string{"recovered"}}, 2, 0},
		{"recover-last-attempt", "textDocument/references", []any{rpcFailure(-32801), rpcFailure(-32801), []string{"recovered"}}, 3, 0},
		{"exhaust", "textDocument/references", []any{rpcFailure(-32801)}, 3, -32801},
		{"internal-error", "textDocument/references", []any{rpcFailure(-32603)}, 1, -32603},
		{"unsupported", "textDocument/references", []any{rpcFailure(-32601)}, 1, -32601},
		{"server-cancelled", "textDocument/references", []any{rpcFailure(-32802)}, 1, -32802},
		{"request-cancelled", "textDocument/references", []any{rpcFailure(-32800)}, 1, -32800},
		{"lifecycle", "shutdown", []any{rpcFailure(-32801)}, 1, -32801},
		{"unknown-mutation", "workspace/executeCommand", []any{rpcFailure(-32801)}, 1, -32801},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := contentModifiedPeer(t, test.method, test.replies)
			before := atomic.LoadInt64(&client.nextID)
			ctx, cancel := context.WithTimeout(context.Background(), lifecycleSetup)
			defer cancel()
			var result []string
			err := client.Call(ctx, test.method, nil, &result)
			if test.code == 0 {
				if err != nil || len(result) != 1 || result[0] != "recovered" {
					t.Fatalf("result=%v err=%v", result, err)
				}
			} else {
				var responseErr *ResponseError
				if !errors.As(err, &responseErr) || responseErr.Code != test.code {
					t.Fatalf("err=%v, want RPC code %d", err, test.code)
				}
			}
			if attempts := atomic.LoadInt64(&client.nextID) - before; attempts != test.attempts {
				t.Errorf("fresh request IDs=%d, want %d", attempts, test.attempts)
			}
			client.mu.Lock()
			pending := len(client.pending)
			client.mu.Unlock()
			if pending != 0 {
				t.Errorf("leaked %d pending requests", pending)
			}
		})
	}
}

func TestContentModifiedDeadlineAndCancellation(t *testing.T) {
	for _, test := range []string{"request-budget", "parent-deadline", "cancelled"} {
		t.Run(test, func(t *testing.T) {
			client := contentModifiedPeer(t, "textDocument/references", []any{rpcFailure(-32801)})
			ctx, cancel := context.WithTimeout(context.Background(), lifecycleSetup)
			want := context.DeadlineExceeded
			switch test {
			case "request-budget":
				// Less than the 150 ms total backoff: a reset budget would
				// incorrectly exhaust retries with ContentModified instead.
				client.requestTimeout = 90 * time.Millisecond
			case "parent-deadline":
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 90*time.Millisecond)
			case "cancelled":
				want = context.Canceled
				cancel()
			}
			defer cancel()
			before := atomic.LoadInt64(&client.nextID)
			if err := client.Call(ctx, "textDocument/references", nil, nil); !errors.Is(err, want) {
				t.Fatalf("err=%v, want %v", err, want)
			}
			attempts := atomic.LoadInt64(&client.nextID) - before
			if attempts > 2 || (test == "cancelled" && attempts != 0) {
				t.Errorf("unexpected requests after deadline/cancellation: %d", attempts)
			}
		})
	}
}
