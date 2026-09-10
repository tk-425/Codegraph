package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tk-425/Codegraph/internal/lsp"
)

func sampleDiagnostics() []lsp.LSPDiagnostic {
	return []lsp.LSPDiagnostic{
		{
			Language:   "java",
			Executable: "jdtls",
			Category:   lsp.MissingExecutable,
			Severity:   lsp.SeverityError,
			Reason:     "executable file not found in $PATH",
			Guidance:   &lsp.InstallationGuidance{Documentation: "https://github.com/eclipse-jdtls/eclipse.jdt.ls"},
		},
		{
			Language:   "go",
			Executable: "gopls",
			Category:   lsp.InitializationFailure,
			Severity:   lsp.SeverityError,
			Reason:     "initialize timed out",
			Guidance:   &lsp.InstallationGuidance{Command: "go install golang.org/x/tools/gopls@latest"},
		},
		{
			Language:   "rust",
			Executable: "rust-analyzer",
			Category:   lsp.ServerLog,
			Severity:   lsp.SeverityWarning,
			Reason:     "unresolved import in crate root",
		},
	}
}

func TestBuildDiagnosticRecordsCarrySeverityForEveryCategory(t *testing.T) {
	records := buildDiagnosticRecords(sampleDiagnostics())
	if len(records) != 3 {
		t.Fatalf("got %d records, want 3", len(records))
	}

	byCategory := make(map[string]buildDiagnosticRecord, len(records))
	for _, record := range records {
		if record.Severity == "" {
			t.Errorf("record %+v has no severity", record)
		}
		byCategory[record.Category] = record
	}

	if got := byCategory[string(lsp.MissingExecutable)].Severity; got != string(lsp.SeverityError) {
		t.Errorf("missing_executable severity = %q, want error", got)
	}
	if got := byCategory[string(lsp.InitializationFailure)].Severity; got != string(lsp.SeverityError) {
		t.Errorf("initialization_failure severity = %q, want error", got)
	}

	serverLog := byCategory[string(lsp.ServerLog)]
	if serverLog.Severity != string(lsp.SeverityWarning) {
		t.Errorf("server_log severity = %q, want warning", serverLog.Severity)
	}
	if serverLog.Command != "" || serverLog.DocumentationURL != "" {
		t.Errorf("server-log record carries guidance %+v, want none", serverLog)
	}
}

func TestPrintDiagnosticsRendersServerLogAsReportedProblem(t *testing.T) {
	var out bytes.Buffer
	printDiagnostics(&out, sampleDiagnostics()[2:])
	rendered := out.String()

	if strings.Contains(rendered, "LSP unavailable") {
		t.Errorf("server-log record rendered as unavailability: %q", rendered)
	}
	if !strings.Contains(rendered, "unresolved import in crate root") {
		t.Errorf("rendering omits the reason: %q", rendered)
	}
	if !strings.Contains(rendered, string(lsp.SeverityWarning)) {
		t.Errorf("rendering omits the severity: %q", rendered)
	}
}

func TestPrintDiagnosticsStillRendersStartupFailuresAsUnavailable(t *testing.T) {
	var out bytes.Buffer
	printDiagnostics(&out, sampleDiagnostics()[:1])
	rendered := out.String()

	if !strings.Contains(rendered, "java LSP unavailable") {
		t.Errorf("startup failure lost its unavailability line: %q", rendered)
	}
	if !strings.Contains(rendered, "Documentation: https://github.com/eclipse-jdtls/eclipse.jdt.ls") {
		t.Errorf("startup failure lost its installation guidance: %q", rendered)
	}
}

func TestBuildEmitsOneEnvelopeWithFixedKeys(t *testing.T) {
	var out bytes.Buffer
	if err := EmitJSON(&out, "build", nil, buildDiagnosticRecords(sampleDiagnostics()), nil); err != nil {
		t.Fatalf("EmitJSON: %v", err)
	}

	decoder := json.NewDecoder(bytes.NewReader(out.Bytes()))
	var envelope map[string]json.RawMessage
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if decoder.More() {
		t.Error("stdout carries more than one envelope")
	}

	for _, key := range []string{"command", "query", "count", "results", "errors"} {
		if _, ok := envelope[key]; !ok {
			t.Errorf("envelope is missing the %q key", key)
		}
	}
	if string(envelope["count"]) != "3" {
		t.Errorf("count = %s, want 3", envelope["count"])
	}
}
