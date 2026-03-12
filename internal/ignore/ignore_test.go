package ignore_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tk-425/Codegraph/internal/ignore"
	"github.com/tk-425/Codegraph/internal/indexer"
)

func TestCreateDefaultCGIgnoreSeedsGitignore(t *testing.T) {
	projectRoot := t.TempDir()
	codegraphDir := filepath.Join(projectRoot, ".codegraph")
	if err := os.MkdirAll(codegraphDir, 0o755); err != nil {
		t.Fatalf("mkdir .codegraph: %v", err)
	}

	gitignoreContent := "generated/\n*.gen.go\n"
	if err := os.WriteFile(filepath.Join(projectRoot, ".gitignore"), []byte(gitignoreContent), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}

	if err := ignore.CreateDefaultCGIgnore(codegraphDir, projectRoot); err != nil {
		t.Fatalf("CreateDefaultCGIgnore: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(codegraphDir, ".cgignore"))
	if err != nil {
		t.Fatalf("read .cgignore: %v", err)
	}

	content := string(data)
	if want := "# Imported from .gitignore"; !strings.Contains(content, want) {
		t.Fatalf(".cgignore missing import header %q\n%s", want, content)
	}
	if want := gitignoreContent; !strings.Contains(content, want) {
		t.Fatalf(".cgignore missing gitignore content %q\n%s", want, content)
	}
}

func TestScannerUsesSeededCGIgnore(t *testing.T) {
	projectRoot := t.TempDir()
	codegraphDir := filepath.Join(projectRoot, ".codegraph")
	if err := os.MkdirAll(codegraphDir, 0o755); err != nil {
		t.Fatalf("mkdir .codegraph: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(projectRoot, "generated"), 0o755); err != nil {
		t.Fatalf("mkdir generated: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(projectRoot, "src"), 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}

	if err := os.WriteFile(filepath.Join(codegraphDir, ".cgignore"), []byte("generated/\n"), 0o644); err != nil {
		t.Fatalf("write .cgignore: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "generated", "tmp.go"), []byte("package generated\n"), 0o644); err != nil {
		t.Fatalf("write generated file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "src", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write src file: %v", err)
	}

	scanner, err := indexer.NewScanner(projectRoot, filepath.Join(codegraphDir, ".cgignore"))
	if err != nil {
		t.Fatalf("NewScanner: %v", err)
	}

	files, err := scanner.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	if len(files) != 1 || files[0].RelPath != "src/main.go" {
		t.Fatalf("unexpected files: %#v", files)
	}
}

func TestScannerSupportsNegationWithoutSkippingDir(t *testing.T) {
	projectRoot := t.TempDir()
	codegraphDir := filepath.Join(projectRoot, ".codegraph")
	if err := os.MkdirAll(codegraphDir, 0o755); err != nil {
		t.Fatalf("mkdir .codegraph: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(projectRoot, "generated"), 0o755); err != nil {
		t.Fatalf("mkdir generated: %v", err)
	}

	patterns := "generated/\n!generated/include.go\n"
	if err := os.WriteFile(filepath.Join(codegraphDir, ".cgignore"), []byte(patterns), 0o644); err != nil {
		t.Fatalf("write .cgignore: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "generated", "skip.go"), []byte("package generated\n"), 0o644); err != nil {
		t.Fatalf("write skip.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "generated", "include.go"), []byte("package generated\n"), 0o644); err != nil {
		t.Fatalf("write include.go: %v", err)
	}

	scanner, err := indexer.NewScanner(projectRoot, filepath.Join(codegraphDir, ".cgignore"))
	if err != nil {
		t.Fatalf("NewScanner: %v", err)
	}

	files, err := scanner.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	if len(files) != 1 || files[0].RelPath != "generated/include.go" {
		t.Fatalf("unexpected files: %#v", files)
	}
}
