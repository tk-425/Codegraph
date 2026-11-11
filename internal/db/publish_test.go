package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(filepath.Join(t.TempDir(), "codegraph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	if err := m.Initialize(); err != nil {
		t.Fatal(err)
	}
	return m
}

func seedSymbol(t *testing.T, m *Manager, id, name, file string, line int) *Symbol {
	t.Helper()
	s := &Symbol{ID: id, Name: name, Kind: "function", File: file, Line: line, Column: 0, Language: "go", Source: "tree-sitter", CreatedAt: time.Now()}
	if err := m.InsertSymbol(s); err != nil {
		t.Fatal(err)
	}
	return s
}

// T20: a plan that fails partway publishes nothing. The failure here is a
// real foreign-key rejection inside the publication transaction.
func TestPublishRollsBackOnFailure(t *testing.T) {
	m := newTestManager(t)
	target := seedSymbol(t, m, "go:a.go#Target", "Target", "/proj/a.go", 1)
	caller := seedSymbol(t, m, "go:a.go#Caller", "Caller", "/proj/a.go", 10)
	if err := m.InsertCall(&Call{CallerID: caller.ID, CalleeID: target.ID, File: "/proj/a.go", Line: 11, Column: 1}); err != nil {
		t.Fatal(err)
	}

	plan := &RefreshPlan{
		ChangedFiles:   []string{"/proj/a.go"},
		DirtyLanguages: []string{"go"},
		Symbols: []*Symbol{
			{ID: "go:b.go#New", Name: "New", Kind: "function", File: "/proj/b.go", Line: 1, Language: "go", CreatedAt: time.Now()},
		},
		// This call names a symbol the plan never inserts: the insert
		// fails inside the transaction and must roll the whole plan back.
		Calls: []*Call{{CallerID: "go:b.go#New", CalleeID: "go:missing#Gone", File: "/proj/b.go", Line: 2}},
	}
	if err := m.Publish(context.Background(), plan); err == nil {
		t.Fatal("publish accepted a call with a missing endpoint")
	}

	stats, err := m.GetStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.SymbolCount != 2 || stats.CallCount != 1 {
		t.Errorf("after failed publish stats = %d symbols, %d calls; want the published 2, 1", stats.SymbolCount, stats.CallCount)
	}
	if symbols, err := m.GetSymbolByName("Target", nil); err != nil || len(symbols) != 1 {
		t.Errorf("published Target lost after failed publish: %+v, %v", symbols, err)
	}
	if symbols, err := m.GetSymbolByName("New", nil); err != nil || len(symbols) != 0 {
		t.Errorf("partially published New survived rollback: %+v, %v", symbols, err)
	}
}

// A dirty language's whole edge set is replaced; a changed file's symbols
// are replaced file-wide, so an obsolete declaration cannot survive.
func TestPublishReplacesChangedFileAndDirtyEdges(t *testing.T) {
	m := newTestManager(t)
	old := seedSymbol(t, m, "go:a.go#Target", "Target", "/proj/a.go", 1)
	keeper := seedSymbol(t, m, "go:c.go#Keeper", "Keeper", "/proj/c.go", 1)
	if err := m.InsertCall(&Call{CallerID: keeper.ID, CalleeID: old.ID, File: "/proj/c.go", Line: 2, Column: 1}); err != nil {
		t.Fatal(err)
	}
	if err := m.RecordFileMeta(FileMeta{Path: "/proj/a.go", ModTime: time.Now(), Language: "go"}); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	plan := &RefreshPlan{
		ChangedFiles:   []string{"/proj/a.go"},
		DirtyLanguages: []string{"go"},
		Symbols: []*Symbol{
			{ID: "go:a.go#Replacement", Name: "Replacement", Kind: "function", File: "/proj/a.go", Line: 1, Language: "go", Source: "tree-sitter", CreatedAt: now},
		},
		FileMeta: []FileMeta{{Path: "/proj/a.go", ModTime: now, Language: "go"}},
	}
	if err := m.Publish(context.Background(), plan); err != nil {
		t.Fatal(err)
	}

	if symbols, err := m.GetSymbolByName("Target", nil); err != nil || len(symbols) != 0 {
		t.Errorf("obsolete Target survived replacement: %+v, %v", symbols, err)
	}
	if symbols, err := m.GetSymbolByName("Replacement", nil); err != nil || len(symbols) != 1 {
		t.Errorf("replacement not published: %+v, %v", symbols, err)
	}
	if symbols, err := m.GetSymbolByName("Keeper", nil); err != nil || len(symbols) != 1 {
		t.Errorf("untouched file's symbol lost: %+v, %v", symbols, err)
	}
	stats, err := m.GetStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.CallCount != 0 {
		t.Errorf("dirty language's old edge survived: %d calls, want 0", stats.CallCount)
	}
	meta, err := m.GetFileMeta("/proj/a.go")
	if err != nil || meta == nil || meta.Language != "go" {
		t.Errorf("changed file metadata not republished: %+v, %v", meta, err)
	}
}

// A removed file (deleted, renamed, moved, newly ignored) loses its symbols
// and metadata, and nothing is inserted for it.
func TestPublishRemovesFileEntirely(t *testing.T) {
	m := newTestManager(t)
	gone := seedSymbol(t, m, "go:a.go#Target", "Target", "/proj/a.go", 1)
	caller := seedSymbol(t, m, "go:b.go#Caller", "Caller", "/proj/b.go", 1)
	if err := m.InsertCall(&Call{CallerID: caller.ID, CalleeID: gone.ID, File: "/proj/b.go", Line: 2, Column: 1}); err != nil {
		t.Fatal(err)
	}
	if err := m.RecordFileMeta(FileMeta{Path: "/proj/a.go", ModTime: time.Now(), Language: "go"}); err != nil {
		t.Fatal(err)
	}

	plan := &RefreshPlan{
		RemovedFiles:   []string{"/proj/a.go"},
		DirtyLanguages: []string{"go"},
	}
	if err := m.Publish(context.Background(), plan); err != nil {
		t.Fatal(err)
	}

	if symbols, err := m.GetSymbolByName("Target", nil); err != nil || len(symbols) != 0 {
		t.Errorf("removed file's symbol survived: %+v, %v", symbols, err)
	}
	if meta, err := m.GetFileMeta("/proj/a.go"); err != nil || meta != nil {
		t.Errorf("removed file's metadata survived: %+v, %v", meta, err)
	}
	stats, err := m.GetStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.CallCount != 0 {
		t.Errorf("edge to removed file survived: %d calls, want 0", stats.CallCount)
	}
}

// T21: a cancelled publication writes nothing.
func TestPublishCancelledContextWritesNothing(t *testing.T) {
	m := newTestManager(t)
	seedSymbol(t, m, "go:a.go#Target", "Target", "/proj/a.go", 1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	plan := &RefreshPlan{
		ChangedFiles:   []string{"/proj/a.go"},
		DirtyLanguages: []string{"go"},
		Symbols: []*Symbol{
			{ID: "go:a.go#New", Name: "New", Kind: "function", File: "/proj/a.go", Line: 1, Language: "go", CreatedAt: time.Now()},
		},
	}
	if err := m.Publish(ctx, plan); err == nil {
		t.Fatal("cancelled publication reported success")
	}
	if symbols, err := m.GetSymbolByName("New", nil); err != nil || len(symbols) != 0 {
		t.Errorf("cancelled publication wrote symbols: %+v, %v", symbols, err)
	}
	if symbols, err := m.GetSymbolByName("Target", nil); err != nil || len(symbols) != 1 {
		t.Errorf("published graph changed under cancellation: %+v, %v", symbols, err)
	}
}

// A calls table created before the site-uniqueness constraint gets its
// duplicates collapsed and the constraint enforced from then on.
func TestInitializeRepairsCallsUniqueness(t *testing.T) {
	m := newTestManager(t)
	seedSymbol(t, m, "go:a.go#Target", "Target", "/proj/a.go", 1)
	caller := seedSymbol(t, m, "go:a.go#Caller", "Caller", "/proj/a.go", 10)
	dup := &Call{CallerID: caller.ID, CalleeID: "go:a.go#Target", File: "/proj/a.go", Line: 11, Column: 1}

	// Recreate the pre-constraint shape: no UNIQUE, no index.
	if _, err := m.db.Exec(`DROP INDEX IF EXISTS idx_calls_site`); err != nil {
		t.Fatal(err)
	}
	if _, err := m.db.Exec(`DROP TABLE calls`); err != nil {
		t.Fatal(err)
	}
	if _, err := m.db.Exec(`
		CREATE TABLE calls (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			caller_id TEXT NOT NULL,
			callee_id TEXT NOT NULL,
			file TEXT NOT NULL,
			line INTEGER NOT NULL,
			column INTEGER NOT NULL,
			FOREIGN KEY(caller_id) REFERENCES symbols(id),
			FOREIGN KEY(callee_id) REFERENCES symbols(id)
		)`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := m.db.Exec(`INSERT INTO calls (caller_id, callee_id, file, line, column) VALUES (?, ?, ?, ?, ?)`,
			dup.CallerID, dup.CalleeID, dup.File, dup.Line, dup.Column); err != nil {
			t.Fatal(err)
		}
	}

	if err := m.Initialize(); err != nil {
		t.Fatal(err)
	}
	stats, err := m.GetStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.CallCount != 1 {
		t.Fatalf("after repair call count = %d, want 1", stats.CallCount)
	}
	if err := m.InsertCall(dup); err != nil {
		t.Fatalf("duplicate insert after repair: %v", err)
	}
	stats, err = m.GetStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.CallCount != 1 {
		t.Errorf("duplicate row accumulated after repair: %d, want 1", stats.CallCount)
	}
}

// SC7: a format-2 database created before the fingerprint columns is
// extended in place by the build path — no format bump, no rebuild — and
// the columns older binaries name still accept their narrower writes.
func TestInitializeExtendsFormat2FileMeta(t *testing.T) {
	m := newTestManager(t)

	// Recreate the v0.8.x shape of file_meta.
	if _, err := m.db.Exec(`DROP TABLE file_meta`); err != nil {
		t.Fatal(err)
	}
	if _, err := m.db.Exec(`CREATE TABLE file_meta (path TEXT PRIMARY KEY, mod_time TIMESTAMP NOT NULL, language TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := m.db.Exec(`INSERT INTO file_meta (path, mod_time, language) VALUES ('a.go', 1000, 'go')`); err != nil {
		t.Fatal(err)
	}
	if _, err := m.db.Exec(`DROP TABLE provider_state`); err != nil {
		t.Fatal(err)
	}

	if err := m.Initialize(); err != nil {
		t.Fatal(err)
	}
	var version int
	if err := m.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != FormatVersion {
		t.Fatalf("format version = %d, err=%v; want %d (no bump)", version, err, FormatVersion)
	}
	// The pre-existing row survives with an empty hash, which never skips.
	meta, err := m.GetFileMeta("a.go")
	if err != nil || meta == nil || meta.ContentHash != "" {
		t.Fatalf("extended row = %+v, %v; want empty hash", meta, err)
	}
	// An older binary's narrower write still fits the extended table.
	if _, err := m.db.Exec(`INSERT INTO file_meta (path, mod_time, language) VALUES ('b.go', 2000, 'go')`); err != nil {
		t.Fatalf("pre-extension statement rejected: %v", err)
	}
	if _, err := m.db.Exec(`INSERT OR REPLACE INTO provider_state (language, digest) VALUES ('go', 'd')`); err != nil {
		t.Fatalf("provider_state not created: %v", err)
	}
}
