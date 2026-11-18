package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tk-425/Codegraph/internal/config"
	"github.com/tk-425/Codegraph/internal/db"
)

// Drive the human build path, not JSON or a hand-rendered snapshot. A no-op
// extracts no records but must still report the retained published graph.
func TestBuildOutputOverlappingProviders(t *testing.T) {
	root := newSyntaxProject(t, map[string]string{
		"source.go": "package fixture\nfunc Target() {}\nfunc Caller() { Target() }\n",
	})
	// The peer returns one semantic call and one failed request. Syntax
	// fallback finds that same call; publication must retain only one edge.
	responses := map[string]any{
		"textDocument/references@1": []any{map[string]any{
			"uri": "${ROOT_URI}/source.go",
			"range": map[string]any{
				"start": map[string]int{"line": 2, "character": 16},
				"end":   map[string]int{"line": 2, "character": 22},
			},
		}},
		"textDocument/references@2": map[string]any{"__error__": map[string]any{
			"code": -32801, "message": "content modified",
		}},
	}
	data, err := json.Marshal(responses)
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(t.TempDir(), "responses.json")
	if err := os.WriteFile(fixture, data, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.LSP["go"] = config.LSPConfig{Command: buildProtocolPeer(t), Args: []string{"-responses", fixture}}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	result := runCLI(t, root, "build")
	if result.exit != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", result.exit, result.stdout, result.stderr)
	}
	manager, err := db.OpenReadOnly(cfg.GetDatabasePath(root))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	snapshot, err := manager.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot == nil || snapshot.Counts.Calls != 1 {
		t.Fatalf("want one deduplicated published edge: %+v", snapshot)
	}
	semantic, syntax, failed := 0, 0, 0
	for _, phase := range snapshot.Phases {
		if phase.Phase == "calls" {
			failed += phase.FailedRequests
			if phase.Provider == "lsp" {
				semantic += phase.Records
			} else if phase.Provider == "tree-sitter" {
				syntax += phase.Records
			}
		}
	}
	if semantic != 1 || syntax != 1 || failed != 1 {
		t.Fatalf("overlap precondition not reached: %+v", snapshot.Phases)
	}
	output := string(result.stdout)
	if strings.Contains(output, "LSP unavailable") || !strings.Contains(output, "content modified") {
		t.Errorf("individual RPC failure misrepresented: %s", output)
	}
	if !regexp.MustCompile(`(?im)^.*published.*\b1 calls\b`).MatchString(output) || strings.Contains(output, "Found 2 call relationships") {
		t.Errorf("two provider observations misrepresented as published edges: %s", output)
	}
}

func TestBuildOutputContentModifiedRecovery(t *testing.T) {
	root := newSyntaxProject(t, map[string]string{
		"source.go": "package fixture\nfunc Target() {}\nfunc Caller() { Target() }\n",
	})
	responses := map[string]any{
		"textDocument/references@1": map[string]any{"__sequence__": []any{
			map[string]any{"__error__": map[string]any{
				"code": -32801, "message": "content modified",
			}},
			[]any{map[string]any{
				"uri": "${ROOT_URI}/source.go",
				"range": map[string]any{
					"start": map[string]int{"line": 2, "character": 16},
					"end":   map[string]int{"line": 2, "character": 22},
				},
			}},
		}},
	}
	data, err := json.Marshal(responses)
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(t.TempDir(), "responses.json")
	if err := os.WriteFile(fixture, data, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.LSP["go"] = config.LSPConfig{Command: buildProtocolPeer(t), Args: []string{"-responses", fixture}}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	result := runCLI(t, root, "build")
	if result.exit != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", result.exit, result.stdout, result.stderr)
	}
	manager, err := db.OpenReadOnly(cfg.GetDatabasePath(root))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	snapshot, err := manager.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot == nil || snapshot.Counts.Calls != 1 {
		t.Fatalf("want one published edge: %+v", snapshot)
	}
	semantic, failed := 0, 0
	for _, phase := range snapshot.Phases {
		if phase.Phase == "calls" {
			failed += phase.FailedRequests
			if phase.Provider == "lsp" {
				semantic += phase.Records
			}
		}
	}
	if semantic != 1 || failed != 0 {
		t.Errorf("transient ContentModified was not recovered: semantic=%d failed=%d phases=%+v", semantic, failed, snapshot.Phases)
	}
	if strings.Contains(string(result.stdout), "LSP request failure") || strings.Contains(string(result.stdout), "content modified") {
		t.Errorf("recovered request reported as failed: %s", result.stdout)
	}
}

func TestBuildOutputExplainsDegradedSnapshot(t *testing.T) {
	root := newSyntaxProject(t, map[string]string{
		"source.go": "package fixture\ntype Node struct{}\nfunc Target() {}\nfunc Caller() { Target() }\n",
	})
	// Symbols and calls succeed; an unsupported hierarchy request must still
	// have a human-readable explanation, including when its snapshot is reused.
	fixture := filepath.Join(t.TempDir(), "responses.json")
	responses := `{
		"textDocument/documentSymbol": [
			{"name":"Node","kind":23,"range":{"start":{"line":1,"character":0},"end":{"line":1,"character":18}},"selectionRange":{"start":{"line":1,"character":5},"end":{"line":1,"character":9}}},
			{"name":"Target","kind":12,"range":{"start":{"line":2,"character":0},"end":{"line":2,"character":16}},"selectionRange":{"start":{"line":2,"character":5},"end":{"line":2,"character":11}}},
			{"name":"Caller","kind":12,"range":{"start":{"line":3,"character":0},"end":{"line":3,"character":26}},"selectionRange":{"start":{"line":3,"character":5},"end":{"line":3,"character":11}}}
		],
		"textDocument/references@2": [{"uri":"${ROOT_URI}/source.go","range":{"start":{"line":3,"character":16},"end":{"line":3,"character":22}}}],
		"textDocument/prepareTypeHierarchy": {"__error__":{"code":-32601,"message":"type hierarchy unsupported"}}
	}`
	if err := os.WriteFile(fixture, []byte(responses), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.LSP["go"] = config.LSPConfig{Command: buildProtocolPeer(t), Args: []string{"-responses", fixture}}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	var baselineID string
	for _, name := range []string{"full", "no-op"} {
		t.Run(name, func(t *testing.T) {
			result := runCLI(t, root, "build")
			if result.exit != 0 || len(result.stderr) != 0 {
				t.Fatalf("exit=%d stdout=%s stderr=%s", result.exit, result.stdout, result.stderr)
			}
			manager, err := db.OpenReadOnly(cfg.GetDatabasePath(root))
			if err != nil {
				t.Fatal(err)
			}
			defer manager.Close()
			snapshot, err := manager.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			if snapshot == nil || snapshot.Quality != "degraded" || snapshot.Counts.Symbols != 3 || snapshot.Counts.Calls != 1 {
				t.Fatalf("fixture did not reach degraded publication with semantic records: %+v", snapshot)
			}
			attempt, err := manager.LatestAttempt()
			if err != nil {
				t.Fatal(err)
			}
			if attempt == nil || attempt.Outcome != "succeeded" {
				t.Fatalf("missing successful attempt: %+v", attempt)
			}
			if name == "full" {
				baselineID = snapshot.ID
				if !attempt.Changed {
					t.Fatal("first build did not publish a changed snapshot")
				}
			} else if baselineID == "" || snapshot.ID != baselineID || attempt.Changed {
				t.Fatalf("fixture did not reuse publication: baseline=%s snapshot=%+v attempt=%+v", baselineID, snapshot, attempt)
			}
			semanticSymbols, semanticCalls := false, false
			var unsupported *db.PhaseOutcome
			for _, phase := range snapshot.Phases {
				if phase.FailedRequests != 0 || phase.Status == "failed" {
					t.Fatalf("fixture unexpectedly failed a provider request: %+v", snapshot.Phases)
				}
				if phase.Provider != "lsp" {
					continue
				}
				switch phase.Phase {
				case "symbols":
					semanticSymbols = phase.Status == "complete" && phase.Records == 3
				case "calls":
					semanticCalls = phase.Status == "complete" && phase.Records == 1
				case "hierarchy":
					if phase.Status == "unsupported" {
						copy := phase
						unsupported = &copy
					}
				}
			}
			if !semanticSymbols || !semanticCalls || unsupported == nil || unsupported.Reason != "provider does not support required requests" {
				t.Fatalf("unsupported-hierarchy precondition not reached: %+v", snapshot.Phases)
			}
			output := string(result.stdout)
			if !strings.Contains(output, "degraded") {
				t.Fatalf("fixture did not print degraded quality: %s", output)
			}
			explained := false
			for _, line := range strings.Split(output, "\n") {
				if strings.Contains(line, unsupported.Language) && strings.Contains(line, unsupported.Phase) &&
					strings.Contains(line, unsupported.Provider) && strings.Contains(line, unsupported.Status) && strings.Contains(line, unsupported.Reason) {
					explained = true
				}
			}
			if !explained {
				t.Errorf("degraded snapshot lacks its stored hierarchy explanation: phase=%+v\nstdout=%s\nstderr=%s", *unsupported, output, result.stderr)
			}
		})
	}
}

func TestBuildOutputUnsupportedCFallback(t *testing.T) {
	root := newSyntaxProject(t, map[string]string{
		"source.c": "int target(void) { return 1; }\nint caller(void) { return target(); }\n",
	})
	result := runCLI(t, root, "build")
	if result.exit != 0 || len(result.stderr) != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", result.exit, result.stdout, result.stderr)
	}
	manager, err := db.OpenReadOnly(config.DefaultConfig().GetDatabasePath(root))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	snapshot, err := manager.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot == nil || snapshot.Counts.Symbols != 2 || snapshot.Counts.Calls != 0 || snapshot.Quality != "degraded" {
		t.Fatalf("C fixture did not recover symbols without calls: %+v", snapshot)
	}
	unsupportedCalls := false
	for _, phase := range snapshot.Phases {
		if phase.Language == "c" && phase.Phase == "calls" && phase.Provider == "tree-sitter" && phase.Status == "unsupported" && phase.Records == 0 {
			unsupportedCalls = true
		}
	}
	if !unsupportedCalls {
		t.Fatalf("C calls fallback was not recorded as unsupported: %+v", snapshot.Phases)
	}
	output := string(result.stdout)
	if regexp.MustCompile(`(?m)^.*\bc\b.*using tree-sitter`).MatchString(output) {
		t.Errorf("C call warning promises an unsupported fallback:\n%s", output)
	}
	explained := false
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "c") && strings.Contains(line, "calls") && strings.Contains(line, "tree-sitter") && strings.Contains(line, "unsupported") {
			explained = true
		}
	}
	if !explained {
		t.Errorf("human output does not explain unsupported C call extraction: phases=%+v\nstdout=%s\nstderr=%s", snapshot.Phases, output, result.stderr)
	}
}

func TestBuildOutputDistinguishesWorkFromPublishedGraph(t *testing.T) {
	root := newSyntaxProject(t, map[string]string{
		"source.go": "package fixture\nfunc Target() {}\nfunc Caller() { Target() }\n",
	})
	var baseline *db.Stats
	for _, run := range []struct {
		name string
		args []string
	}{
		{"full", []string{"build"}},
		{"incremental", []string{"build"}},
		{"force", []string{"build", "--force"}},
	} {
		t.Run(run.name, func(t *testing.T) {
			result := runCLI(t, root, run.args...)
			if result.exit != 0 {
				t.Fatalf("exit=%d stdout=%s stderr=%s", result.exit, result.stdout, result.stderr)
			}
			manager, err := db.OpenReadOnly(config.DefaultConfig().GetDatabasePath(root))
			if err != nil {
				t.Fatal(err)
			}
			defer manager.Close()
			stats, err := manager.GetStats()
			if err != nil {
				t.Fatal(err)
			}
			if stats.SymbolCount == 0 || stats.CallCount == 0 {
				t.Fatalf("fixture did not publish symbols and calls: %+v", stats)
			}
			if baseline == nil {
				baseline = stats
			} else if stats.SymbolCount != baseline.SymbolCount || stats.CallCount != baseline.CallCount {
				t.Errorf("graph changed without input changes: baseline=%+v current=%+v", baseline, stats)
			}
			output := string(result.stdout)
			published := regexp.MustCompile(fmt.Sprintf(`(?im)^.*published.*\b%d symbols\b.*\b%d calls\b`, stats.SymbolCount, stats.CallCount))
			if !published.MatchString(output) {
				t.Errorf("no labelled published totals matching stored graph (%d symbols, %d calls):\n%s", stats.SymbolCount, stats.CallCount, output)
			}
			if regexp.MustCompile(`Found \d+ call relationships`).MatchString(output) {
				t.Errorf("unqualified extraction count presented as graph total:\n%s", output)
			}
			if run.name == "incremental" && !strings.Contains(output, "skipped 1 unchanged") {
				t.Errorf("fixture did not reach unchanged-file reuse:\n%s", output)
			}
		})
	}
}
