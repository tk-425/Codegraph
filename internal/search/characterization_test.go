//go:build characterization

package search

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/tk-425/Codegraph/internal/db"
	"github.com/tk-425/Codegraph/internal/indexer"
)

func characterizationSyntaxDatabase(t *testing.T) (*db.Manager, string) {
	t.Helper()
	root := t.TempDir()
	content, err := os.ReadFile(filepath.Join("..", "..", "testdata", "characterization", "refresh", "before.go"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "source.go")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	database, err := db.NewManager(filepath.Join(root, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := database.Initialize(); err != nil {
		t.Fatal(err)
	}
	file := indexer.FileInfo{Path: path, RelPath: "source.go", Language: "go"}
	if _, err := indexer.NewTreeSitterIndexer(database, root).IndexFile(context.Background(), file); err != nil {
		t.Fatal(err)
	}
	symbols, err := database.GetSymbolByName("Target", nil)
	if err != nil || len(symbols) != 1 || symbols[0].Source != "tree-sitter" {
		t.Fatalf("syntax fixture provenance: symbols=%+v, err=%v", symbols, err)
	}
	return database, symbols[0].Source
}

func TestCharacterizationControlStoredProvenance(t *testing.T) {
	database, source := characterizationSyntaxDatabase(t)
	if source != "tree-sitter" || NewDatabaseTier(database).Name() != "database" {
		t.Fatal("fixture must distinguish extraction provider from storage tier")
	}
}

func TestCharacterizationF07SearchPreservesExtractionProvider(t *testing.T) {
	database, source := characterizationSyntaxDatabase(t)
	results, err := NewDatabaseTier(database).Search(context.Background(), SearchOptions{Query: "Target"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("search results=%+v, want one Target", results)
	}
	if results[0].Source != source {
		t.Errorf("T31: storage tier replaced extraction provenance: got %q, want %q", results[0].Source, source)
	}
}
