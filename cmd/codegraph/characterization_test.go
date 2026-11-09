//go:build characterization

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/tk-425/Codegraph/internal/config"
)

// Re-execute the test binary in a fresh process, entering the real main().
// Exit directly so Go's test-status text cannot contaminate CLI stdout.
func TestS0CLIHelperProcess(t *testing.T) {
	if os.Getenv("CODEGRAPH_S0_CLI_CHILD") != "1" {
		return
	}
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		os.Exit(2)
	}
	os.Args = append([]string{"codegraph"}, os.Args[separator+1:]...)
	main()
	os.Exit(0)
}

type characterizationCLIResult struct {
	stdout []byte
	stderr []byte
	exit   int
}

func runCharacterizationCLI(t *testing.T, root string, args ...string) characterizationCLIResult {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	childArgs := append([]string{"-test.run=^TestS0CLIHelperProcess$", "--"}, args...)
	command := exec.CommandContext(ctx, binary, childArgs...)
	command.Dir = root
	command.Env = append(os.Environ(), "CODEGRAPH_S0_CLI_CHILD=1", "HOME="+root)
	command.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err = command.Run()
	if ctx.Err() != nil {
		t.Fatalf("CLI watchdog expired for %v; stderr=%s", args, stderr.String())
	}
	var exitError *exec.ExitError
	if err != nil && !errors.As(err, &exitError) {
		t.Fatalf("execute CLI: %v", err)
	}
	return characterizationCLIResult{stdout.Bytes(), stderr.Bytes(), command.ProcessState.ExitCode()}
}

type characterizationEnvelope struct {
	Count   int               `json:"count"`
	Results []json.RawMessage `json:"results"`
	Errors  []struct {
		Code string `json:"code"`
	} `json:"errors"`
}

func requireCharacterizationEnvelope(t *testing.T, result characterizationCLIResult) characterizationEnvelope {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(result.stdout))
	var raw map[string]json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		t.Fatalf("T33/T34: stdout is not one JSON envelope: %v; stdout=%q; stderr=%q; exit=%d", err, result.stdout, result.stderr, result.exit)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("extra stdout after envelope: value=%v, err=%v", extra, err)
	}
	for _, key := range []string{"command", "query", "count", "results", "errors"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("envelope missing %q", key)
		}
	}
	var envelope characterizationEnvelope
	if err := json.Unmarshal(result.stdout, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Count != len(envelope.Results) {
		t.Errorf("count=%d does not equal result records=%d", envelope.Count, len(envelope.Results))
	}
	return envelope
}

func TestCharacterizationControlJSONProcess(t *testing.T) {
	result := runCharacterizationCLI(t, t.TempDir(), "health", "--json")
	if result.exit != 0 {
		t.Fatalf("diagnostic exit=%d; stderr=%s", result.exit, result.stderr)
	}
	envelope := requireCharacterizationEnvelope(t, result)
	if len(envelope.Errors) != 0 || envelope.Count == 0 {
		t.Fatalf("expected diagnostic records, not operational errors: %+v", envelope)
	}
}

func TestCharacterizationF08ValidationEmitsJSON(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{"missing_argument", []string{"search", "--json"}},
		{"unknown_flag", []string{"search", "Target", "--json", "--s0-unknown"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := runCharacterizationCLI(t, t.TempDir(), test.args...)
			if result.exit == 0 {
				t.Error("invalid invocation returned success")
			}
			envelope := requireCharacterizationEnvelope(t, result)
			if len(envelope.Errors) == 0 {
				t.Error("T34: validation failure has no structured error")
			}
		})
	}
}

func TestCharacterizationF08BuildConfigFailureEmitsJSON(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, ".codegraph")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte("[broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := runCharacterizationCLI(t, root, "build", "--json")
	if result.exit == 0 {
		t.Error("invalid config returned success")
	}
	envelope := requireCharacterizationEnvelope(t, result)
	if len(envelope.Errors) == 0 {
		t.Error("T34: build configuration failure has no structured error")
	}
}

func TestCharacterizationF08QueryDoesNotCreateMissingDatabase(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".codegraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	result := runCharacterizationCLI(t, root, "signature", "Target", "--json")
	if result.exit == 0 {
		t.Error("missing database query returned success")
	}
	envelope := requireCharacterizationEnvelope(t, result)
	if len(envelope.Errors) != 1 || envelope.Errors[0].Code != "database_missing" {
		t.Errorf("T34: missing database error=%+v, want database_missing", envelope.Errors)
	}
	graphs := filepath.Join(root, ".codegraph", "graphs")
	if _, err := os.Stat(graphs); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("T34: read-only query created database artifacts at %s (stat=%v)", graphs, err)
	}
}

func TestCharacterizationF01CLIEmptyTreeReconciles(t *testing.T) {
	for _, force := range []bool{false, true} {
		name := "incremental"
		if force {
			name = "force"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			cfg := config.DefaultConfig()
			cfg.LSP["go"] = config.LSPConfig{Command: filepath.Join(root, "unavailable-lsp")}
			if err := config.Save(root, cfg); err != nil {
				t.Fatal(err)
			}
			ignore := filepath.Join(root, ".codegraph", ".cgignore")
			if err := os.WriteFile(ignore, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(filepath.Join("..", "..", "testdata", "characterization", "refresh", "before.go"))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "source.go"), content, 0o644); err != nil {
				t.Fatal(err)
			}
			initial := runCharacterizationCLI(t, root, "build", "--json")
			if initial.exit != 0 {
				t.Fatalf("initial build failed: %s", initial.stderr)
			}
			requireCharacterizationEnvelope(t, initial)
			before := runCharacterizationCLI(t, root, "signature", "Target", "--json")
			if before.exit != 0 || requireCharacterizationEnvelope(t, before).Count != 1 {
				t.Fatal("fixture did not publish Target before the empty scan")
			}
			if err := os.WriteFile(ignore, []byte("*.go\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			args := []string{"build", "--json"}
			if force {
				args = append(args, "--force")
			}
			refresh := runCharacterizationCLI(t, root, args...)
			if refresh.exit != 0 {
				t.Fatalf("empty-tree build exit=%d; stderr=%s", refresh.exit, refresh.stderr)
			}
			requireCharacterizationEnvelope(t, refresh)
			after := runCharacterizationCLI(t, root, "signature", "Target", "--json")
			if after.exit != 0 {
				t.Fatalf("query after empty build exit=%d; stderr=%s", after.exit, after.stderr)
			}
			if envelope := requireCharacterizationEnvelope(t, after); envelope.Count != 0 {
				t.Errorf("T04: CLI empty-tree build retained %d obsolete Target records", envelope.Count)
			}
		})
	}
}
