package db

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenReadOnlyDoesNotCreateMissingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graphs", "codegraph.db")
	if _, err := OpenReadOnly(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want os.ErrNotExist", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("I09: read-only open created %s (stat=%v)", filepath.Dir(path), err)
	}
}

func TestOpenReadOnlyReadsButRejectsWrites(t *testing.T) {
	// Spaces, '#', '%', '?' and non-ASCII must survive URI encoding (T40).
	for _, dir := range []string{"plain", "with space", "hash#dir", "pct%dir", "q?dir", "é𝔘dir"} {
		t.Run(dir, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), dir, "codegraph.db")
			writer, err := NewManager(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := writer.Initialize(); err != nil {
				t.Fatal(err)
			}
			writer.Close()

			reader, err := OpenReadOnly(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			if _, err := reader.GetStats(); err != nil {
				t.Fatalf("read: %v", err)
			}
			if err := reader.RecordFileMeta(FileMeta{Path: "x.go", ModTime: time.Now(), Language: "go"}); err == nil {
				t.Fatal("read-only database accepted a write")
			}
		})
	}
}

// I02: every pooled connection enforces foreign keys, so a relationship can
// never name a symbol that is not stored.
func TestForeignKeysHoldOnEveryConnection(t *testing.T) {
	m, err := NewManager(filepath.Join(t.TempDir(), "codegraph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err := m.Initialize(); err != nil {
		t.Fatal(err)
	}
	m.db.SetMaxIdleConns(0) // force a fresh connection per statement
	for i := 0; i < 3; i++ {
		if err := m.InsertCall(&Call{CallerID: "missing", CalleeID: "missing", File: "f", Line: 1}); err == nil {
			t.Fatalf("attempt %d: dangling call endpoint accepted", i)
		}
	}
}
