package lsp

import (
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tk-425/Codegraph/internal/config"
)

func writeTypeScriptPackage(packageRoot, version string) error {
	if err := os.MkdirAll(filepath.Join(packageRoot, "bin"), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(packageRoot, "package.json"), []byte(`{"version":"`+version+`"}`), 0644); err != nil {
		return err
	}
	bin := filepath.Join(filepath.Dir(packageRoot), ".bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(bin, "tsc"), []byte("#!/bin/sh\n"), 0755)
}

func TestManagerRetriesNativeInitializationAtMostThreeTimes(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join(root, "node_modules", "typescript")
	if err := writeTypeScriptPackage(fixture, "7.0.0"); err != nil {
		t.Fatal(err)
	}
	calls, cleanups, sleeps := 0, 0, 0
	oldNew, oldInit, oldCleanup, oldSleep := newLSPClient, initializeLSP, cleanupFailedLSP, sleepBeforeRetry
	defer func() {
		newLSPClient, initializeLSP, cleanupFailedLSP, sleepBeforeRetry = oldNew, oldInit, oldCleanup, oldSleep
	}()
	newLSPClient = func(string, []string, string, string) (*Client, error) { calls++; return &Client{}, nil }
	initializeLSP = func(context.Context, *Client) error { return errors.New("not ready") }
	cleanupFailedLSP = func(*Client) { cleanups++ }
	sleepBeforeRetry = func(time.Duration) { sleeps++ }

	rootURI := (&url.URL{Scheme: "file", Path: root}).String()
	_, err := NewManager(config.DefaultConfig(), rootURI).GetClient(context.Background(), "typescript")
	if err == nil || calls != 3 || cleanups != 3 || sleeps != 2 {
		t.Fatalf("err=%v calls=%d cleanups=%d sleeps=%d", err, calls, cleanups, sleeps)
	}
}

func TestManagerStopsRetryAfterNativeInitializationSucceeds(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join(root, "node_modules", "typescript")
	if err := writeTypeScriptPackage(fixture, "7.0.0"); err != nil {
		t.Fatal(err)
	}
	calls, sleeps := 0, 0
	oldNew, oldInit, oldSleep := newLSPClient, initializeLSP, sleepBeforeRetry
	defer func() { newLSPClient, initializeLSP, sleepBeforeRetry = oldNew, oldInit, oldSleep }()
	newLSPClient = func(string, []string, string, string) (*Client, error) { calls++; return &Client{}, nil }
	initializeLSP = func(context.Context, *Client) error {
		if calls < 2 {
			return errors.New("not ready")
		}
		return nil
	}
	sleepBeforeRetry = func(time.Duration) { sleeps++ }

	rootURI := (&url.URL{Scheme: "file", Path: root}).String()
	client, err := NewManager(config.DefaultConfig(), rootURI).GetClient(context.Background(), "typescript")
	if err != nil || client == nil || calls != 2 || sleeps != 1 {
		t.Fatalf("err=%v client=%v calls=%d sleeps=%d", err, client != nil, calls, sleeps)
	}
}

func TestManagerNativeInitializationSucceedsOnFirstAttempt(t *testing.T) {
	root := t.TempDir()
	if err := writeTypeScriptPackage(filepath.Join(root, "node_modules", "typescript"), "7.0.0"); err != nil {
		t.Fatal(err)
	}
	calls := 0
	oldNew, oldInit := newLSPClient, initializeLSP
	defer func() { newLSPClient, initializeLSP = oldNew, oldInit }()
	newLSPClient = func(string, []string, string, string) (*Client, error) { calls++; return &Client{}, nil }
	initializeLSP = func(context.Context, *Client) error { return nil }

	rootURI := (&url.URL{Scheme: "file", Path: root}).String()
	if _, err := NewManager(config.DefaultConfig(), rootURI).GetClient(context.Background(), "typescript"); err != nil || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestManagerNativeInitializationSucceedsOnThirdAttempt(t *testing.T) {
	root := t.TempDir()
	if err := writeTypeScriptPackage(filepath.Join(root, "node_modules", "typescript"), "7.0.0"); err != nil {
		t.Fatal(err)
	}
	calls := 0
	oldNew, oldInit, oldCleanup, oldSleep := newLSPClient, initializeLSP, cleanupFailedLSP, sleepBeforeRetry
	defer func() {
		newLSPClient, initializeLSP, cleanupFailedLSP, sleepBeforeRetry = oldNew, oldInit, oldCleanup, oldSleep
	}()
	newLSPClient = func(string, []string, string, string) (*Client, error) { calls++; return &Client{}, nil }
	initializeLSP = func(context.Context, *Client) error {
		if calls < 3 {
			return errors.New("not ready")
		}
		return nil
	}
	cleanupFailedLSP = func(*Client) {}
	sleepBeforeRetry = func(time.Duration) {}

	rootURI := (&url.URL{Scheme: "file", Path: root}).String()
	if _, err := NewManager(config.DefaultConfig(), rootURI).GetClient(context.Background(), "typescript"); err != nil || calls != 3 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestManagerDoesNotRetryAfterInitialization(t *testing.T) {
	root := t.TempDir()
	if err := writeTypeScriptPackage(filepath.Join(root, "node_modules", "typescript"), "7.0.0"); err != nil {
		t.Fatal(err)
	}
	calls := 0
	oldNew, oldInit := newLSPClient, initializeLSP
	defer func() { newLSPClient, initializeLSP = oldNew, oldInit }()
	newLSPClient = func(string, []string, string, string) (*Client, error) { calls++; return &Client{}, nil }
	initializeLSP = func(context.Context, *Client) error { return nil }

	rootURI := (&url.URL{Scheme: "file", Path: root}).String()
	manager := NewManager(config.DefaultConfig(), rootURI)
	client, err := manager.GetClient(context.Background(), "typescript")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.GetClient(context.Background(), "typescript"); err != nil || client == nil || calls != 1 {
		t.Fatalf("err=%v client=%v calls=%d", err, client != nil, calls)
	}
}

func TestManagerDiagnosticsMergesStartupFailuresAndServerLogs(t *testing.T) {
	manager := &Manager{
		clients: map[string]*Client{
			"rust": {Language: "rust", logMessages: []LSPDiagnostic{
				{Language: "rust", Executable: "rust-analyzer", Category: ServerLog, Severity: SeverityWarning, Reason: "unresolved import"},
			}},
		},
		diagnostics: map[string]LSPDiagnostic{
			"java": {Language: "java", Executable: "jdtls", Category: MissingExecutable, Severity: SeverityError, Reason: "executable file not found"},
		},
	}

	records := manager.Diagnostics()
	if len(records) != 2 {
		t.Fatalf("got %d records, want the startup failure plus the server log", len(records))
	}

	byCategory := make(map[DiagnosticCategory]LSPDiagnostic, len(records))
	for _, record := range records {
		byCategory[record.Category] = record
	}

	startup, ok := byCategory[MissingExecutable]
	if !ok {
		t.Fatal("missing the startup-failure record")
	}
	if startup.Language != "java" || startup.Severity != SeverityError {
		t.Errorf("startup record = %+v, want java at error severity", startup)
	}

	serverLog, ok := byCategory[ServerLog]
	if !ok {
		t.Fatal("missing the server-log record")
	}
	if serverLog.Language != "rust" || serverLog.Severity != SeverityWarning {
		t.Errorf("server-log record = %+v, want rust at warning severity", serverLog)
	}
	if serverLog.Guidance != nil {
		t.Errorf("guidance = %+v, want none on a server-log record", serverLog.Guidance)
	}

	if second := manager.Diagnostics(); len(second) != 1 {
		t.Errorf("second call returned %d records, want only the startup failure after the drain", len(second))
	}
}

func TestManagerCachesStartupFailureForTheAttempt(t *testing.T) {
	calls := 0
	oldNew := newLSPClient
	defer func() { newLSPClient = oldNew }()
	newLSPClient = func(string, []string, string, string) (*Client, error) {
		calls++
		return nil, errors.New("exec: not found")
	}

	manager := NewManager(config.DefaultConfig(), "file:///project")
	for phase := 0; phase < 3; phase++ {
		if _, err := manager.GetClient(context.Background(), "go"); err == nil {
			t.Fatal("unavailable server reported success")
		}
	}
	if calls != 1 {
		t.Fatalf("server start attempts = %d across three phases, want one per build attempt", calls)
	}
	if records := manager.Diagnostics(); len(records) != 1 {
		t.Fatalf("diagnostics = %+v, want one startup record", records)
	}
}

func TestManagerDoesNotReturnClientWithClosedTransport(t *testing.T) {
	oldNew, oldInit := newLSPClient, initializeLSP
	defer func() { newLSPClient, initializeLSP = oldNew, oldInit }()
	dead := newClient(nopWriteCloser{}, nopReadCloser{}, "", "go")
	newLSPClient = func(string, []string, string, string) (*Client, error) { return dead, nil }
	initializeLSP = func(context.Context, *Client) error { return nil }

	manager := NewManager(config.DefaultConfig(), "file:///project")
	if _, err := manager.GetClient(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	dead.fail(errors.New("server exited"))
	if _, err := manager.GetClient(context.Background(), "go"); !errors.Is(err, ErrTransportClosed) {
		t.Fatalf("err = %v, want the closed transport reported instead of a dead client", err)
	}
}

func TestManagerAppliesConfiguredBudgets(t *testing.T) {
	oldNew, oldInit := newLSPClient, initializeLSP
	defer func() { newLSPClient, initializeLSP = oldNew, oldInit }()
	created := newClient(nopWriteCloser{}, nopReadCloser{}, "", "go")
	newLSPClient = func(string, []string, string, string) (*Client, error) { return created, nil }
	initializeLSP = func(context.Context, *Client) error { return nil }

	cfg := config.DefaultConfig()
	cfg.LSPTimeouts = config.LSPTimeoutsConfig{RequestSeconds: 7, ShutdownGracefulSeconds: 2, ShutdownForceSeconds: 3}
	if _, err := NewManager(cfg, "file:///project").GetClient(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	if created.requestTimeout != 7*time.Second || created.gracefulTimeout != 2*time.Second || created.forceTimeout != 3*time.Second {
		t.Fatalf("budgets = %s/%s/%s, want 7s/2s/3s from config", created.requestTimeout, created.gracefulTimeout, created.forceTimeout)
	}
}

type nopReadCloser struct{}

func (nopReadCloser) Read([]byte) (int, error) { return 0, io.EOF }
func (nopReadCloser) Close() error             { return nil }
