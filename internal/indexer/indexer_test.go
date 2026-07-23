package indexer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tk-425/Codegraph/internal/config"
	"github.com/tk-425/Codegraph/internal/db"
)

func TestScannerRoutesTypeScriptReactJavaScriptAndOtherLanguages(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"component.tsx": "export const view = <div />",
		"script.js":     "function run() {}",
		"main.go":       "package main",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	ignorePath := filepath.Join(root, ".cgignore")
	if err := os.WriteFile(ignorePath, nil, 0644); err != nil {
		t.Fatal(err)
	}
	scanner, err := NewScanner(root, ignorePath)
	if err != nil {
		t.Fatal(err)
	}
	files, err := scanner.Scan()
	if err != nil {
		t.Fatal(err)
	}
	groups := GroupByLanguage(files)
	if len(groups["typescriptreact"]) != 1 || len(groups["typescript"]) != 1 || len(groups["go"]) != 1 {
		t.Fatalf("language groups = %#v", groups)
	}
}

func TestIndexProjectRetriesNativeTypeScriptThenFallsBack(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "example.ts")
	if err := os.WriteFile(filePath, []byte("function greet(name: string) { return name }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	packageRoot := filepath.Join(root, "node_modules", "typescript")
	if err := os.MkdirAll(filepath.Join(root, "node_modules", ".bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(packageRoot, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageRoot, "package.json"), []byte(`{"version":"7.0.0"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", ".bin", "tsc"), []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	database, err := db.NewManager(filepath.Join(root, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Initialize(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := NewIndexer(cfg, database, root).IndexProject(ctx, []FileInfo{{Path: filePath, RelPath: "example.ts", Language: "typescript"}}, true); err != nil {
		t.Fatal(err)
	}
	if meta, err := database.GetFileMeta(filePath); err != nil || meta == nil {
		t.Fatalf("expected fallback metadata, err=%v", err)
	}
}

func TestIndexProjectFallsBackToTreeSitterWhenLSPFails(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "example.ts")
	if err := os.WriteFile(filePath, []byte("function greet(name: string) { return name }\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	cfg.LSP["typescript"] = config.LSPConfig{Command: "missing-typescript-lsp", Args: []string{"--stdio"}}
	database, err := db.NewManager(filepath.Join(root, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Initialize(); err != nil {
		t.Fatal(err)
	}

	indexer := NewIndexer(cfg, database, root)
	if err := indexer.IndexProject(context.Background(), []FileInfo{{Path: filePath, RelPath: "example.ts", Language: "typescript"}}, true); err != nil {
		t.Fatal(err)
	}
	meta, err := database.GetFileMeta(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if meta == nil {
		t.Fatal("expected Tree-sitter fallback to record file metadata")
	}
}
