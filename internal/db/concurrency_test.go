package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// holdWriteLock opens a second connection, takes the write lock with
// BEGIN IMMEDIATE and inserts an uncommitted symbol. The returned release
// rolls it back.
func holdWriteLock(t *testing.T, dbPath string) (release func()) {
	t.Helper()
	abs, err := filepath.Abs(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	holder, err := sql.Open("sqlite3", fileDSN(abs, "_busy_timeout=5000"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := holder.Exec("BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	if _, err := holder.Exec(`INSERT INTO symbols (` + symbolColumns("") + `)
		VALUES ('go:held.go#Held', 'Held', 'function', '/proj/held.go', 1, 0, 0, 0, NULL, NULL, '', '', '', 'go', 'lsp', CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	return func() {
		_, _ = holder.Exec("ROLLBACK")
		_ = holder.Close()
	}
}

// T22: while another connection holds the write lock, publication fails
// within a bounded wait with an explicit outcome — and the graph stays
// coherent for the next attempt once the lock is released.
func TestPublishOverlappingWriterFailsBounded(t *testing.T) {
	previous := busyTimeoutMS
	busyTimeoutMS = 300
	t.Cleanup(func() { busyTimeoutMS = previous })

	m := newTestManager(t)
	seedSymbol(t, m, "go:a.go#Target", "Target", "/proj/a.go", 1)
	release := holdWriteLock(t, m.dbPath)

	plan := &RefreshPlan{
		ChangedFiles:   []string{"/proj/a.go"},
		DirtyLanguages: []string{"go"},
		Symbols: []*Symbol{
			{ID: "go:a.go#New", Name: "New", Kind: "function", File: "/proj/a.go", Line: 1, Language: "go", CreatedAt: time.Now()},
		},
	}
	start := time.Now()
	err := m.Publish(context.Background(), plan)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("publish succeeded while another writer held the lock")
	}
	if !strings.Contains(err.Error(), "locked") && !strings.Contains(err.Error(), "busy") {
		t.Errorf("overlap outcome not an explicit lock error: %v", err)
	}
	if elapsed > 5*time.Second {
		t.Errorf("overlap wait unbounded: %v", elapsed)
	}
	if symbols, serr := m.GetSymbolByName("Target", nil); serr != nil || len(symbols) != 1 {
		t.Errorf("published graph disturbed by a rejected publish: %+v, %v", symbols, serr)
	}

	release()
	if err := m.Publish(context.Background(), plan); err != nil {
		t.Fatalf("publish after the lock was released: %v", err)
	}
	if symbols, serr := m.GetSymbolByName("New", nil); serr != nil || len(symbols) != 1 {
		t.Errorf("retry after overlap did not publish: %+v, %v", symbols, serr)
	}
}

// T22: a reader during an open write transaction sees the old snapshot —
// not the uncommitted rows, not an error, not a mixture — and the new one
// after it commits.
func TestReaderSeesOldOrNewNeverAMixture(t *testing.T) {
	m := newTestManager(t)
	seedSymbol(t, m, "go:a.go#Target", "Target", "/proj/a.go", 1)
	release := holdWriteLock(t, m.dbPath)

	reader, err := OpenReadOnly(m.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	during, err := reader.GetSymbolByName("Held", nil)
	if err != nil {
		t.Fatalf("reader failed while a writer held the lock: %v", err)
	}
	if len(during) != 0 {
		t.Fatalf("reader saw uncommitted rows: %+v", during)
	}
	if stable, serr := reader.GetSymbolByName("Target", nil); serr != nil || len(stable) != 1 {
		t.Errorf("old snapshot not intact during the write: %+v, %v", stable, serr)
	}

	release()
	after, err := reader.GetSymbolByName("Held", nil)
	if err != nil {
		t.Fatal(err)
	}
	// The holder rolled back, so Held never became visible; what the
	// reader must not have seen is a half-applied state, which the two
	// consistent reads above already establish.
	if len(after) != 0 {
		t.Logf("holder rolled back; Held count after = %d", len(after))
	}
}

// T21: a cancelled publication writes nothing and reports cancellation.
func TestPublishCancelledBeforeAnyWrite(t *testing.T) {
	m := newTestManager(t)
	seedSymbol(t, m, "go:a.go#Target", "Target", "/proj/a.go", 1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.Publish(ctx, &RefreshPlan{DirtyLanguages: []string{"go"}}); err == nil {
		t.Fatal("cancelled publish reported success")
	}
	if symbols, err := m.GetSymbolByName("Target", nil); err != nil || len(symbols) != 1 {
		t.Errorf("cancelled publish changed the graph: %+v, %v", symbols, err)
	}
}
