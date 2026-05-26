package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyAgentNoteUpdatesExistingAgentInstructionFile(t *testing.T) {
	projectRoot := t.TempDir()
	path := filepath.Join(projectRoot, "AGENTS.md")
	if err := os.WriteFile(path, []byte("# Agent Instructions\n"), 0o644); err != nil {
		t.Fatalf("write Agent instruction file: %v", err)
	}

	result := applyAgentNote(projectRoot)

	if len(result.Updated) != 1 || result.Updated[0] != "AGENTS.md" {
		t.Fatalf("unexpected updated files: %#v", result.Updated)
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("unexpected skipped files: %#v", result.Skipped)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %#v", result.Warnings)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read Agent instruction file: %v", err)
	}
	content := string(data)
	if count := strings.Count(content, codeGraphAgentNoteStart); count != 1 {
		t.Fatalf("expected one CodeGraph agent note start marker, got %d\n%s", count, content)
	}
	if count := strings.Count(content, codeGraphAgentNoteEnd); count != 1 {
		t.Fatalf("expected one CodeGraph agent note end marker, got %d\n%s", count, content)
	}
}

func TestApplyAgentNoteOnlyUpdatesExistingRootAgentInstructionFiles(t *testing.T) {
	projectRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectRoot, "CLAUDE.md"), []byte("# Claude\n"), 0o644); err != nil {
		t.Fatalf("write root Agent instruction file: %v", err)
	}
	nestedDir := filepath.Join(projectRoot, "docs")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("mkdir nested dir: %v", err)
	}
	nestedPath := filepath.Join(nestedDir, "AGENTS.md")
	if err := os.WriteFile(nestedPath, []byte("# Nested\n"), 0o644); err != nil {
		t.Fatalf("write nested Agent instruction file: %v", err)
	}

	result := applyAgentNote(projectRoot)

	if len(result.Updated) != 1 || result.Updated[0] != "CLAUDE.md" {
		t.Fatalf("unexpected updated files: %#v", result.Updated)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, "GEMINI.md")); !os.IsNotExist(err) {
		t.Fatalf("missing Agent instruction file should remain absent, stat error: %v", err)
	}
	data, err := os.ReadFile(nestedPath)
	if err != nil {
		t.Fatalf("read nested Agent instruction file: %v", err)
	}
	if strings.Contains(string(data), codeGraphAgentNoteStart) {
		t.Fatalf("nested Agent instruction file was modified:\n%s", string(data))
	}
}

func TestApplyAgentNoteSkipsAlreadyConfiguredAgentInstructionFile(t *testing.T) {
	projectRoot := t.TempDir()
	path := filepath.Join(projectRoot, "GEMINI.md")
	original := "# Gemini\n\n" + codeGraphAgentNote
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("write Agent instruction file: %v", err)
	}

	result := applyAgentNote(projectRoot)

	if len(result.Updated) != 0 {
		t.Fatalf("unexpected updated files: %#v", result.Updated)
	}
	if len(result.Skipped) != 1 || result.Skipped[0] != "GEMINI.md" {
		t.Fatalf("unexpected skipped files: %#v", result.Skipped)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read Agent instruction file: %v", err)
	}
	content := string(data)
	if count := strings.Count(content, codeGraphAgentNoteStart); count != 1 {
		t.Fatalf("expected one CodeGraph agent note start marker, got %d\n%s", count, content)
	}
	if count := strings.Count(content, codeGraphAgentNoteEnd); count != 1 {
		t.Fatalf("expected one CodeGraph agent note end marker, got %d\n%s", count, content)
	}
}

func TestApplyAgentNoteReturnsManualCopyBlockWhenNoAgentInstructionFilesExist(t *testing.T) {
	projectRoot := t.TempDir()

	result := applyAgentNote(projectRoot)

	if len(result.Updated) != 0 {
		t.Fatalf("unexpected updated files: %#v", result.Updated)
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("unexpected skipped files: %#v", result.Skipped)
	}
	if result.ManualCopyBlock == "" {
		t.Fatal("expected Manual-copy block")
	}
	if !strings.Contains(result.ManualCopyBlock, codeGraphAgentNoteStart) || !strings.Contains(result.ManualCopyBlock, codeGraphAgentNoteEnd) {
		t.Fatalf("Manual-copy block missing CodeGraph agent note markers:\n%s", result.ManualCopyBlock)
	}
	for _, name := range agentInstructionFiles {
		if _, err := os.Stat(filepath.Join(projectRoot, name)); !os.IsNotExist(err) {
			t.Fatalf("missing Agent instruction file %s should remain absent, stat error: %v", name, err)
		}
	}
}

func TestReportAgentNoteResultPrintsManualCopyBlock(t *testing.T) {
	output := captureStdout(t, func() {
		reportAgentNoteResult(agentNoteResult{ManualCopyBlock: codeGraphAgentNote})
	})

	if !strings.Contains(output, "Manual-copy block") {
		t.Fatalf("expected Manual-copy block label in output:\n%s", output)
	}
	if !strings.Contains(output, codeGraphAgentNoteStart) || !strings.Contains(output, codeGraphAgentNoteEnd) {
		t.Fatalf("expected CodeGraph agent note markers in output:\n%s", output)
	}
}

func TestApplyAgentNoteWarnsAndContinuesAfterPerFileUpdateFailure(t *testing.T) {
	projectRoot := t.TempDir()
	failingPath := filepath.Join(projectRoot, "CLAUDE.md")
	if err := os.Mkdir(failingPath, 0o755); err != nil {
		t.Fatalf("create unreadable Agent instruction file path: %v", err)
	}
	updatablePath := filepath.Join(projectRoot, "AGENTS.md")
	if err := os.WriteFile(updatablePath, []byte("# Agents\n"), 0o644); err != nil {
		t.Fatalf("write updatable Agent instruction file: %v", err)
	}

	result := applyAgentNote(projectRoot)

	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "CLAUDE.md") {
		t.Fatalf("expected CLAUDE.md warning, got %#v", result.Warnings)
	}
	if len(result.Updated) != 1 || result.Updated[0] != "AGENTS.md" {
		t.Fatalf("expected AGENTS.md to continue updating, got %#v", result.Updated)
	}
	data, err := os.ReadFile(updatablePath)
	if err != nil {
		t.Fatalf("read updatable Agent instruction file: %v", err)
	}
	if !strings.Contains(string(data), codeGraphAgentNoteStart) {
		t.Fatalf("expected updatable Agent instruction file to receive CodeGraph agent note:\n%s", string(data))
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	os.Stdout = writer

	fn()

	if err := writer.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	os.Stdout = original

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, reader); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}
	return buf.String()
}
