package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

type cmdCase struct {
	name string
	run  func(*cobra.Command, []string) error
	args []string
}

// negativePathCases enumerates commands whose JSON failure-path is triggered
// by an uninitialized cwd (no .codegraph/). Exclusions:
//   - projects: reads the global registry, not the cwd — covered by
//     TestJSONErrors_ProjectsCorruptRegistry.
//   - health: is a diagnostic that *reports* state, so "not initialized" is
//     a record with ok=false rather than an envelope error — covered by
//     TestJSONErrors_HealthUninitializedDiagnostic.
//   - types: stub that always emits a not_implemented error envelope
//     regardless of cwd state — its error path is asserted by
//     TestJSONSymbol_Types, not here.
func negativePathCases() []cmdCase {
	return []cmdCase{
		{"search", runSearch, []string{"x"}},
		{"signature", runSignature, []string{"x"}},
		{"callers", runCallers, []string{"x"}},
		{"callees", runCallees, []string{"x"}},
		{"implementations", runImplementations, []string{"x"}},
		{"stats", runStats, nil},
	}
}

func TestJSONErrors_UninitializedCwd(t *testing.T) {
	for _, tc := range negativePathCases() {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			jsonOutputFlag = true
			t.Cleanup(func() { jsonOutputFlag = false })

			buf := &bytes.Buffer{}
			c := &cobra.Command{Use: tc.name, RunE: tc.run}
			c.SetOut(buf)
			c.SetErr(buf)

			err := c.RunE(c, tc.args)

			// envelope must be valid JSON regardless of error
			var env map[string]json.RawMessage
			if jerr := json.Unmarshal(buf.Bytes(), &env); jerr != nil {
				t.Fatalf("envelope is not valid JSON: %v\nraw=%s", jerr, buf.String())
			}
			for _, k := range []string{"command", "query", "count", "results", "errors"} {
				if _, ok := env[k]; !ok {
					t.Fatalf("envelope missing key %q: %s", k, buf.String())
				}
			}

			var errs []EnvelopeError
			if uerr := json.Unmarshal(env["errors"], &errs); uerr != nil {
				t.Fatalf("errors unmarshal: %v", uerr)
			}
			if len(errs) == 0 {
				t.Fatalf("expected non-empty errors array, got envelope: %s", buf.String())
			}

			if err == nil {
				t.Fatalf("expected RunE to return non-nil error, got nil; envelope: %s", buf.String())
			}
		})
	}
}

func TestJSONErrors_HealthUninitializedDiagnostic(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	jsonOutputFlag = true
	t.Cleanup(func() { jsonOutputFlag = false })

	buf := &bytes.Buffer{}
	c := &cobra.Command{Use: "health", RunE: runHealth}
	c.SetOut(buf)
	c.SetErr(buf)

	if err := c.RunE(c, nil); err != nil {
		t.Fatalf("health is a diagnostic; uninitialized cwd should not error, got: %v", err)
	}

	var env map[string]json.RawMessage
	if jerr := json.Unmarshal(buf.Bytes(), &env); jerr != nil {
		t.Fatalf("envelope is not valid JSON: %v\nraw=%s", jerr, buf.String())
	}
	for _, k := range []string{"command", "query", "count", "results", "errors"} {
		if _, ok := env[k]; !ok {
			t.Fatalf("envelope missing key %q: %s", k, buf.String())
		}
	}

	var recs []healthRecord
	if uerr := json.Unmarshal(env["results"], &recs); uerr != nil {
		t.Fatalf("results unmarshal: %v", uerr)
	}
	if len(recs) == 0 {
		t.Fatalf("expected at least one health record, got 0: %s", buf.String())
	}
	first := recs[0]
	if first.Category != "initialized" || first.OK {
		t.Errorf("expected first record to be {category:initialized, ok:false}, got %+v", first)
	}
}

func TestJSONErrors_ProjectsCorruptRegistry(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	jsonOutputFlag = true
	t.Cleanup(func() { jsonOutputFlag = false })

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	regDir := filepath.Join(homeDir, ".codegraph")
	if err := os.MkdirAll(regDir, 0o755); err != nil {
		t.Fatalf("mkdir registry dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(regDir, "registry.json"), []byte("not valid json"), 0o644); err != nil {
		t.Fatalf("write corrupt registry: %v", err)
	}

	buf := &bytes.Buffer{}
	c := &cobra.Command{Use: "projects", RunE: runProjects}
	c.SetOut(buf)
	c.SetErr(buf)

	err := c.RunE(c, nil)

	var env map[string]json.RawMessage
	if jerr := json.Unmarshal(buf.Bytes(), &env); jerr != nil {
		t.Fatalf("envelope is not valid JSON: %v\nraw=%s", jerr, buf.String())
	}
	var errs []EnvelopeError
	_ = json.Unmarshal(env["errors"], &errs)
	if len(errs) == 0 {
		t.Fatalf("expected non-empty errors array, got envelope: %s", buf.String())
	}
	if err == nil {
		t.Fatalf("expected RunE to return non-nil error")
	}
}

// regressionPrefix is the literal prefix expected on the first non-empty
// stdout line when each command runs without --json. This is a guard against
// silent removal of decorative output on the human-readable path.
type regressionCase struct {
	name   string
	prefix string
	setup  func(t *testing.T)
	run    func(*cobra.Command, []string) error
	args   []string
}

func TestJSONErrors_NonJSONRegression(t *testing.T) {
	cases := []regressionCase{
		{
			name: "search", prefix: "🔍",
			setup: func(t *testing.T) { setupCodegraphProject(t); jsonOutputFlag = false },
			run:   runSearch, args: []string{"nope"},
		},
		{
			name: "signature", prefix: "📝",
			setup: func(t *testing.T) { setupCodegraphProject(t); jsonOutputFlag = false },
			run:   runSignature, args: []string{"nope"},
		},
		{
			name: "callers", prefix: "📞",
			setup: func(t *testing.T) { setupCodegraphProject(t); jsonOutputFlag = false },
			run:   runCallers, args: []string{"nope"},
		},
		{
			name: "callees", prefix: "📤",
			setup: func(t *testing.T) { setupCodegraphProject(t); jsonOutputFlag = false },
			run:   runCallees, args: []string{"nope"},
		},
		{
			name: "implementations", prefix: "🔧",
			setup: func(t *testing.T) { setupCodegraphProject(t); jsonOutputFlag = false },
			run:   runImplementations, args: []string{"nope"},
		},
		{
			name: "types", prefix: "🔗",
			setup: func(t *testing.T) {
				setupCodegraphProject(t)
				jsonOutputFlag = false
			},
			run: runTypes, args: []string{"nope"},
		},
		{
			name: "health", prefix: "🏥",
			setup: func(t *testing.T) { setupCodegraphProject(t); jsonOutputFlag = false },
			run:   runHealth, args: nil,
		},
		{
			name: "projects", prefix: "📁",
			setup: func(t *testing.T) {
				homeDir := t.TempDir()
				t.Setenv("HOME", homeDir)
				jsonOutputFlag = false
				t.Cleanup(func() { jsonOutputFlag = false })
			},
			run: runProjects, args: nil,
		},
		{
			name: "stats", prefix: "CodeGraph",
			setup: func(t *testing.T) { setupCodegraphProject(t); jsonOutputFlag = false },
			run:   runStats, args: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup(t)
			t.Cleanup(func() { jsonOutputFlag = false })

			c := &cobra.Command{Use: tc.name, RunE: tc.run}
			out := captureStdout(t, func() {
				_ = c.RunE(c, tc.args)
			})

			first := firstNonEmptyLine(out)
			if !strings.HasPrefix(strings.TrimLeft(first, " \t"), tc.prefix) {
				t.Errorf("first non-empty stdout line did not start with %q\nfirst line: %q\nfull output:\n%s", tc.prefix, first, out)
			}
		})
	}
}

func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		// Strip ANSI color codes for prefix matching.
		stripped := stripANSI(line)
		if strings.TrimSpace(stripped) != "" {
			return stripped
		}
	}
	return ""
}

// stripANSI removes ANSI color escape sequences so prefix assertions match
// regardless of whether the color library emitted escape codes.
func stripANSI(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j < len(s) {
				i = j + 1
				continue
			}
			break
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
