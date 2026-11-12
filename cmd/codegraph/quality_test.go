package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/tk-425/Codegraph/internal/config"
	"github.com/tk-425/Codegraph/internal/db"
)

type qualityOutput struct {
	Results  []json.RawMessage `json:"results"`
	Metadata struct {
		Snapshot  *db.Snapshot      `json:"snapshot"`
		Attempt   *db.BuildAttempt  `json:"attempt"`
		Freshness string            `json:"freshness"`
		Live      []db.PhaseOutcome `json:"live"`
	} `json:"metadata"`
}

func decodeQuality(t *testing.T, result cliResult) qualityOutput {
	t.Helper()
	requireOneEnvelope(t, result)
	var output qualityOutput
	if err := json.Unmarshal(result.stdout, &output); err != nil {
		t.Fatal(err)
	}
	return output
}

func qualityProject(t *testing.T) (string, *config.Config) {
	t.Helper()
	root := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.LSP["go"] = config.LSPConfig{Command: filepath.Join(root, "missing-lsp")}
	cfg.LSPTimeouts = config.LSPTimeoutsConfig{RequestSeconds: 1, ShutdownGracefulSeconds: 1, ShutdownForceSeconds: 1}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".codegraph", ".cgignore"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "source.go"), []byte("package fixture\nfunc Target() {}\nfunc Caller() { Target() }\n// commentOnly\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, cfg
}

func TestStoredQualityAndProvenanceProcess(t *testing.T) {
	root, _ := qualityProject(t)
	old := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(root, "source.go"), old, old); err != nil {
		t.Fatal(err)
	}
	built := runCLI(t, root, "build", "--json")
	if built.exit != 0 {
		t.Fatalf("build: %s %s", built.stdout, built.stderr)
	}
	publication := decodeQuality(t, built)
	snapshot := publication.Metadata.Snapshot
	if snapshot == nil || snapshot.Quality != "degraded" || publication.Metadata.Attempt == nil || publication.Metadata.Attempt.Outcome != "succeeded" {
		t.Fatalf("missing quality: %s", built.stdout)
	}
	for _, command := range []string{"search", "signature", "callers"} {
		t.Run(command, func(t *testing.T) {
			result := runCLI(t, root, command, "Target", "--json")
			output := decodeQuality(t, result)
			if result.exit != 0 || output.Metadata.Snapshot == nil || output.Metadata.Snapshot.ID != snapshot.ID || output.Metadata.Freshness != "unchecked" || len(output.Results) != 1 {
				t.Fatalf("query: %s %s", result.stdout, result.stderr)
			}
			var record struct {
				ID, Source, Origin string
				Evidence           []string
			}
			if err := json.Unmarshal(output.Results[0], &record); err != nil {
				t.Fatal(err)
			}
			if record.ID == "" || record.Source != "tree-sitter" || record.Origin != "stored" {
				t.Fatalf("record=%+v", record)
			}
			if command == "callers" && (len(record.Evidence) != 1 || record.Evidence[0] != "tree-sitter-call") {
				t.Fatalf("call evidence=%+v", record.Evidence)
			}
		})
	}
	stats := decodeQuality(t, runCLI(t, root, "stats", "--json"))
	var record struct {
		LastBuildTime time.Time `json:"last_build_time"`
	}
	if err := json.Unmarshal(stats.Results[0], &record); err != nil {
		t.Fatal(err)
	}
	if !record.LastBuildTime.Equal(snapshot.PublishedAt) || record.LastBuildTime.Equal(old) {
		t.Fatalf("stats time=%s publication=%s", record.LastBuildTime, snapshot.PublishedAt)
	}
	noop := decodeQuality(t, runCLI(t, root, "build", "--json"))
	if noop.Metadata.Snapshot.ID != snapshot.ID || !noop.Metadata.Snapshot.PublishedAt.Equal(snapshot.PublishedAt) || noop.Metadata.Attempt.Changed {
		t.Fatalf("no-op changed publication: %+v", noop.Metadata)
	}
	if err := os.WriteFile(filepath.Join(root, "source.go"), []byte("package fixture\nfunc Changed() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unchecked := decodeQuality(t, runCLI(t, root, "search", "Target", "--json"))
	if unchecked.Metadata.Freshness != "unchecked" || unchecked.Metadata.Snapshot.ID != snapshot.ID {
		t.Fatalf("disk edits falsely claimed checked: %+v", unchecked.Metadata)
	}
	healthResult := runCLI(t, root, "health", "--json")
	health := decodeQuality(t, healthResult)
	found := false
	for _, raw := range health.Results {
		var r struct {
			Category string
			OK       bool
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			t.Fatal(err)
		}
		if r.Category == "snapshot" {
			found = true
			if r.OK {
				t.Fatal("degraded snapshot claimed healthy")
			}
		}
	}
	if healthResult.exit != 0 || !found {
		t.Fatalf("health contract: %s", healthResult.stdout)
	}
}

func TestTextResultIsNotStoredSymbol(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("real ripgrep unavailable; text fallback not verified")
	}
	root, _ := qualityProject(t)
	if result := runCLI(t, root, "build", "--json"); result.exit != 0 {
		t.Fatalf("build: %s", result.stdout)
	}
	result := runCLI(t, root, "search", "commentOnly", "--json")
	output := decodeQuality(t, result)
	if result.exit != 0 || len(output.Results) != 1 {
		t.Fatalf("text search: %s %s", result.stdout, result.stderr)
	}
	var record struct{ ID, Kind, Source, Origin, Context string }
	if err := json.Unmarshal(output.Results[0], &record); err != nil {
		t.Fatal(err)
	}
	if record.ID != "" || record.Kind != "match" || record.Source != "ripgrep" || record.Origin != "text" || record.Context == "" {
		t.Fatalf("text masquerades as symbol: %+v", record)
	}
}

func implementationProject(t *testing.T, command string, args []string) (string, *config.Config) {
	t.Helper()
	root, cfg := qualityProject(t)
	cfg.LSP["go"] = config.LSPConfig{Command: command, Args: args}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	m, err := db.NewManager(cfg.GetDatabasePath(root))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err := m.Initialize(); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "source.go")
	if err := m.Publish(context.Background(), &db.RefreshPlan{ChangedFiles: []string{file}, Symbols: []*db.Symbol{{ID: "go:source.go#Reader", Name: "Reader", Kind: "interface", File: file, Line: 2, Language: "go", Source: "tree-sitter", CreatedAt: time.Now()}}, Phases: []db.PhaseOutcome{{Language: "go", Phase: "symbols", Provider: "tree-sitter", Status: "fallback"}}}); err != nil {
		t.Fatal(err)
	}
	return root, cfg
}

func TestLiveImplementationOutcomes(t *testing.T) {
	peer := buildProtocolPeer(t)
	for _, test := range []struct {
		name, fixture string
		fail          bool
		status        string
	}{
		{"success", `{"textDocument/implementation":[{"uri":"${ROOT_URI}/source.go","range":{"start":{"line":2,"character":0},"end":{"line":2,"character":1}}}]}`, false, "complete"},
		{"empty", `{"textDocument/implementation":[]}`, false, "complete"},
		{"rpc failure", `{"textDocument/implementation":{"__error__":{"code":-32603,"message":"scripted failure"}}}`, true, "failed"},
		{"unsupported", `{"textDocument/implementation":{"__error__":{"code":-32601,"message":"unsupported"}}}`, true, "unsupported"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := filepath.Join(t.TempDir(), "response.json")
			if err := os.WriteFile(fixture, []byte(test.fixture), 0o644); err != nil {
				t.Fatal(err)
			}
			root, _ := implementationProject(t, peer, []string{"-responses", fixture})
			result := runCLI(t, root, "implementations", "Reader", "--json")
			output := decodeQuality(t, result)
			if test.fail {
				requireErrorCode(t, result, "implementations_lookup_failed")
			} else if result.exit != 0 {
				t.Fatalf("unexpected failure: %s %s", result.stdout, result.stderr)
			}
			if len(output.Metadata.Live) != 1 || output.Metadata.Live[0].Status != test.status {
				t.Fatalf("live outcome=%+v", output.Metadata.Live)
			}
			for _, raw := range output.Results {
				var r struct{ Source, Origin string }
				if err := json.Unmarshal(raw, &r); err != nil {
					t.Fatal(err)
				}
				if r.Source != "lsp" || r.Origin != "live" {
					t.Fatalf("unlabelled live result: %s", raw)
				}
			}
		})
	}
	t.Run("startup failure", func(t *testing.T) {
		root, _ := implementationProject(t, filepath.Join(t.TempDir(), "missing-peer"), nil)
		result := runCLI(t, root, "implementations", "Reader", "--json")
		requireErrorCode(t, result, "implementations_lookup_failed")
		output := decodeQuality(t, result)
		if len(output.Metadata.Live) != 1 || output.Metadata.Live[0].Status != "failed" {
			t.Fatalf("startup outcome=%+v", output.Metadata.Live)
		}
	})
}

func TestLiveImplementationCancellationReapsPeer(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "peer.pid")
	root, cfg := implementationProject(t, buildProtocolPeer(t), []string{"-mode", "block-after-initialize", "-pidfile", pidfile})
	cfg.LSPTimeouts.RequestSeconds = 30
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	command := cliCommand(t, root, "implementations", "Reader", "--json")
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
	// The pidfile proves a provider was launched. Cancellation may interrupt
	// initialization or its first RPC; both must propagate and reap the peer.
	if err := command.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("cancelled lookup did not exit")
	}
	requireErrorCode(t, cliResult{stdout.Bytes(), stderr.Bytes(), command.ProcessState.ExitCode()}, "cancelled")
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Errorf("peer survived cancellation: %v", err)
	}
}
