package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The probe response follows the peer's progress notifications on the same
// stream, so receiving it establishes that the client has read those events.
func progressPeer(t *testing.T, mode string) *Client {
	t.Helper()
	client := startLifecyclePeer(t, "work-done-progress-"+mode)
	ctx, cancel := context.WithTimeout(context.Background(), lifecycleSetup)
	defer cancel()
	if err := client.Call(ctx, "probe", nil, nil); err != nil {
		t.Fatal(err)
	}
	return client
}

func TestProgressQueriesWait(t *testing.T) {
	for _, token := range []string{"string", "integer"} {
		t.Run(token, func(t *testing.T) {
			client := progressPeer(t, "active-"+token)
			client.requestTimeout = lifecycleBudget
			before := atomic.LoadInt64(&client.nextID)
			_, err := client.DocumentSymbols(context.Background(), "file:///fixture.go")
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("query during observed work: %v, want deadline without sending", err)
			}
			if atomic.LoadInt64(&client.nextID) != before {
				t.Fatal("query allocated an RPC while progress remained active")
			}
			client.requestTimeout = lifecycleSetup
			ctx, cancel := context.WithTimeout(context.Background(), lifecycleSetup)
			defer cancel()
			result := make(chan error, 1)
			go func() {
				_, err := client.DocumentSymbols(ctx, "file:///fixture.go")
				result <- err
			}()
			// This control RPC must bypass the query wait or it deadlocks.
			if err := client.Call(ctx, "progress/end", nil, nil); err != nil {
				t.Fatal(err)
			}
			if err := <-result; err != nil {
				t.Fatalf("query after progress end: %v", err)
			}
		})
	}
}

func TestProgressWaitInterrupts(t *testing.T) {
	t.Run("parent-deadline", func(t *testing.T) {
		client := progressPeer(t, "active-string")
		ctx, cancel := context.WithTimeout(context.Background(), lifecycleBudget)
		defer cancel()
		_, err := client.DocumentSymbols(ctx, "file:///fixture.go")
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("waiting query: %v, want parent deadline", err)
		}
	})
	t.Run("cancellation", func(t *testing.T) {
		client := progressPeer(t, "active-string")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		result := make(chan error, 1)
		go func() {
			_, err := client.DocumentSymbols(ctx, "file:///fixture.go")
			result <- err
		}()
		cancel()
		select {
		case err := <-result:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("waiting query: %v, want cancellation", err)
			}
		case <-time.After(lifecycleSetup):
			t.Fatal("cancellation did not release query")
		}
	})
	t.Run("transport-closure", func(t *testing.T) {
		client := progressPeer(t, "active-string")
		ctx, cancel := context.WithTimeout(context.Background(), lifecycleSetup)
		defer cancel()
		result := make(chan error, 1)
		go func() {
			_, err := client.DocumentSymbols(ctx, "file:///fixture.go")
			result <- err
		}()
		// exit is a notification: it must not wait for active work.
		if err := client.Notify("exit", nil); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-result:
			if !errors.Is(err, ErrTransportClosed) {
				t.Fatalf("waiting query: %v, want transport closure", err)
			}
		case <-ctx.Done():
			t.Fatal("transport closure did not release query")
		}
	})
}

func TestProgressBeforeContentModifiedRetry(t *testing.T) {
	client := progressPeer(t, "retry-string")
	client.requestTimeout = lifecycleBudget
	before := atomic.LoadInt64(&client.nextID)
	_, err := client.DocumentSymbols(context.Background(), "file:///fixture.go")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("progress during retry = %v, want shared deadline", err)
	}
	if attempts := atomic.LoadInt64(&client.nextID) - before; attempts != 1 {
		t.Fatalf("sent %d query attempts while update remained active, want 1", attempts)
	}
	client.requestTimeout = lifecycleSetup
	ctx, cancel := context.WithTimeout(context.Background(), lifecycleSetup)
	defer cancel()
	if err := client.Call(ctx, "progress/end", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := client.DocumentSymbols(ctx, "file:///fixture.go"); err != nil {
		t.Fatalf("query after update: %v", err)
	}
}

func TestProgressCreatedTokenDoesNotWait(t *testing.T) {
	client := progressPeer(t, "created-string")
	ctx, cancel := context.WithTimeout(context.Background(), lifecycleSetup)
	defer cancel()
	if _, err := client.DocumentSymbols(ctx, "file:///fixture.go"); err != nil {
		t.Fatalf("unused created token blocked query: %v", err)
	}
}

func TestProgressInvalidCreate(t *testing.T) {
	client := startLifecyclePeer(t, "work-done-progress-invalid")
	ctx, cancel := context.WithTimeout(context.Background(), lifecycleSetup)
	defer cancel()
	var observation struct {
		Creation struct {
			Error *ResponseError `json:"error"`
		} `json:"creation"`
	}
	if err := client.Call(ctx, "probe", nil, &observation); err != nil {
		t.Fatal(err)
	}
	if observation.Creation.Error == nil || observation.Creation.Error.Code != -32602 {
		t.Fatalf("invalid create response: %+v", observation.Creation)
	}
	if _, err := client.DocumentSymbols(ctx, "file:///fixture.go"); err != nil {
		t.Fatalf("invalid create damaged transport: %v", err)
	}
}

// waitObservedContext marks entry into the blocking select without a sleep.
// It tests the wait seam; the query tests above separately exercise framing.
type waitObservedContext struct {
	context.Context
	once sync.Once
	seen chan struct{}
}

func (c *waitObservedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.seen) })
	return c.Context.Done()
}

func TestProgressState(t *testing.T) {
	client := newClient(nil, nil, "", "go")
	observe := func(params string) { client.progress.observe(json.RawMessage(params)) }
	assertActive := func(want int) {
		t.Helper()
		client.progress.mu.Lock()
		defer client.progress.mu.Unlock()
		if got := len(client.progress.active); got != want {
			t.Fatalf("active operations = %d, want %d", got, want)
		}
	}
	for _, invalid := range []string{
		`{`, `null`, `{"token":null,"value":{"kind":"begin","title":"x"}}`,
		`{"token":true,"value":{"kind":"begin","title":"x"}}`,
		`{"token":1.5,"value":{"kind":"begin","title":"x"}}`,
		`{"token":2147483648,"value":{"kind":"begin","title":"x"}}`,
		`{"token":"x","value":{"kind":"begin"}}`,
		`{"token":"x","value":[{"name":"partial result"}]}`,
		`{"token":"x","value":{"kind":"report","percentage":100}}`,
		`{"token":"x","value":{"kind":"end"}}`,
	} {
		observe(invalid)
		assertActive(0)
	}
	observe(`{"token":7,"value":{"kind":"begin","title":"numeric"}}`)
	observe(`{"token":"7","value":{"kind":"begin","title":"string"}}`)
	observe(`{"token":7,"value":{"kind":"begin","title":"duplicate"}}`)
	observe(`{"token":7,"value":{"kind":"report","percentage":100}}`)
	assertActive(2)

	ctx, cancel := context.WithTimeout(context.Background(), lifecycleSetup)
	defer cancel()
	const waiters = 4
	results := make(chan error, waiters)
	for range waiters {
		observed := &waitObservedContext{Context: ctx, seen: make(chan struct{})}
		go func() { results <- client.waitForProgress(observed) }()
		select {
		case <-observed.seen:
		case <-ctx.Done():
			t.Fatal("waiter did not enter active-progress wait")
		}
	}
	observe(`{"token":7,"value":{"kind":"end"}}`)
	assertActive(1)
	observe(`{"token":7,"value":{"kind":"end"}}`)
	assertActive(1)
	observe(`{"token":"7","value":{"kind":"end"}}`)
	assertActive(0)
	for range waiters {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
}

func TestProgressEnteredWaitInterrupts(t *testing.T) {
	for _, closeTransport := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "close"}[closeTransport], func(t *testing.T) {
			client := newClient(nil, nil, "", "go")
			client.progress.observe(json.RawMessage(`{"token":"x","value":{"kind":"begin","title":"work"}}`))
			parent, cancel := context.WithTimeout(context.Background(), lifecycleSetup)
			defer cancel()
			ctx := &waitObservedContext{Context: parent, seen: make(chan struct{})}
			result := make(chan error, 1)
			go func() { result <- client.waitForProgress(ctx) }()
			select {
			case <-ctx.seen:
			case <-parent.Done():
				t.Fatal("did not enter wait")
			}
			want := context.Canceled
			if closeTransport {
				client.fail(errors.New("test transport failure"))
				want = ErrTransportClosed
			} else {
				cancel()
			}
			select {
			case err := <-result:
				if !errors.Is(err, want) {
					t.Fatalf("wait = %v, want %v", err, want)
				}
			case <-time.After(lifecycleSetup):
				t.Fatal("wait was not interrupted")
			}
		})
	}
}

func TestProgressProtocol(t *testing.T) {
	for _, token := range []string{"string", "integer"} {
		t.Run(token, func(t *testing.T) {
			client := startLifecyclePeer(t, "work-done-progress-"+token)
			ctx, cancel := context.WithTimeout(context.Background(), lifecycleSetup)
			defer cancel()
			var observation struct {
				Initialize struct {
					Capabilities struct {
						Window struct {
							WorkDoneProgress bool `json:"workDoneProgress"`
						} `json:"window"`
					} `json:"capabilities"`
				} `json:"initialize"`
				Creation struct {
					ID     string          `json:"id"`
					Result json.RawMessage `json:"result"`
					Error  *ResponseError  `json:"error"`
				} `json:"creation"`
			}
			if err := client.Call(ctx, "probe", nil, &observation); err != nil {
				t.Fatal(err)
			}
			if !observation.Initialize.Capabilities.Window.WorkDoneProgress {
				t.Error("initialize did not advertise window.workDoneProgress")
			}
			if observation.Creation.ID != "create-progress" {
				t.Errorf("create response ID = %q", observation.Creation.ID)
			}
			if observation.Creation.Error != nil {
				t.Errorf("create rejected: %v", observation.Creation.Error)
			}
			if string(observation.Creation.Result) != "null" {
				t.Errorf("create result bytes = %v (length %d), want explicit null", []byte(observation.Creation.Result), len(observation.Creation.Result))
			}
		})
	}
}
