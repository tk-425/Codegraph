package lsp

import (
	"context"
	"errors"
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
