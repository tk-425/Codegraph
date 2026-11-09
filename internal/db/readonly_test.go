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
	// Spaces, '#' and '%' must survive URI encoding. A literal '?' is a known
	// go-sqlite3 DSN limit shared with read-write opens (path work is R09).
	for _, dir := range []string{"plain", "with space", "hash#dir", "pct%dir"} {
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
			if err := reader.UpdateFileMeta("x.go", time.Now(), "go"); err == nil {
				t.Fatal("read-only database accepted a write")
			}
		})
	}
}
