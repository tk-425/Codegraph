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
