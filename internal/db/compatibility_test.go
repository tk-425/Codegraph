package db

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// This fixture omits S4a's optional reporting schema. The process-level suite
// additionally exercises a real v0.9.0 executable, not just these SQL contracts.
func formatTwoProject(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	for _, statement := range []string{
		CreateSymbolsTable,
		`CREATE TABLE calls(id INTEGER PRIMARY KEY AUTOINCREMENT, caller_id TEXT NOT NULL, callee_id TEXT NOT NULL, file TEXT NOT NULL, line INTEGER NOT NULL, column INTEGER NOT NULL, UNIQUE(caller_id,callee_id,file,line,column), FOREIGN KEY(caller_id) REFERENCES symbols(id), FOREIGN KEY(callee_id) REFERENCES symbols(id))`,
		`CREATE TABLE type_hierarchy(id INTEGER PRIMARY KEY AUTOINCREMENT,child_id TEXT NOT NULL,parent_id TEXT NOT NULL,relationship TEXT NOT NULL,FOREIGN KEY(child_id) REFERENCES symbols(id),FOREIGN KEY(parent_id) REFERENCES symbols(id))`,
		CreateFileMetaTable, CreateProviderStateTable, CreateUnresolvedTable, CreateIndexes, `PRAGMA user_version = 2`,
	} {
		if _, err := m.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	a := seedSymbol(t, m, "go:a.go#A", "A", "/a.go", 1)
	b := seedSymbol(t, m, "go:a.go#B", "B", "/a.go", 2)
	if _, err := m.db.Exec(`INSERT INTO calls(caller_id,callee_id,file,line,column) VALUES(?,?,?,?,?)`, a.ID, b.ID, a.File, 3, 0); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := m.db.Exec(`INSERT INTO type_hierarchy(child_id,parent_id,relationship) VALUES(?,?,?)`, a.ID, b.ID, "implements"); err != nil {
			t.Fatal(err)
		}
	}
	return m
}

func TestFormatTwoOptionalReadsDoNotMigrate(t *testing.T) {
	writer := formatTwoProject(t)
	before, err := os.ReadFile(writer.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := OpenReadOnly(writer.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if err := reader.CheckFormat(); err != nil {
		t.Fatal(err)
	}
	if err := reader.PinRead(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := reader.Snapshot()
	if err != nil || snapshot != nil {
		t.Fatalf("legacy snapshot=%+v error=%v", snapshot, err)
	}
	attempt, err := reader.LatestAttempt()
	if err != nil || attempt != nil {
		t.Fatalf("legacy attempt=%+v error=%v", attempt, err)
	}
	callers, err := reader.GetCallers("B", nil)
	if err != nil || len(callers) != 1 || callers[0].Evidence != "unknown" {
		t.Fatalf("legacy callers=%+v error=%v", callers, err)
	}
	callees, err := reader.GetCallees("A", nil)
	if err != nil || len(callees) != 1 || callees[0].Evidence != "unknown" {
		t.Fatalf("legacy callees=%+v error=%v", callees, err)
	}
	implementations, err := reader.GetImplementationsByName("B", nil)
	if err != nil || len(implementations) != 1 || implementations[0].RelationshipEvidence != "unknown" {
		t.Fatalf("legacy hierarchy=%+v error=%v", implementations, err)
	}
	stats, err := reader.GetDetailedStats()
	if err != nil || stats.LastBuildTime != nil {
		t.Fatalf("invented legacy publication time: %+v error=%v", stats, err)
	}
	after, err := os.ReadFile(writer.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("read-only compatibility queries changed database bytes")
	}
	for _, table := range []string{"quality_state", "current_snapshot", "build_attempts"} {
		present, err := reader.tableExists(table)
		if err != nil || present {
			t.Fatalf("query created %s: %v %v", table, present, err)
		}
	}
}

func TestBuildExtensionKeepsVersionAndLegacyHierarchyWrites(t *testing.T) {
	m := formatTwoProject(t)
	if err := m.Initialize(); err != nil {
		t.Fatal(err)
	}
	var version, rows int
	if err := m.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := m.db.QueryRow(`SELECT COUNT(*) FROM type_hierarchy`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if version != 2 || rows != 2 {
		t.Fatalf("extension changed format/data: version=%d hierarchy rows=%d", version, rows)
	}
	// An older writer's duplicate INSERT remains valid; no new UNIQUE constraint.
	if _, err := m.db.Exec(`INSERT INTO type_hierarchy(child_id,parent_id,relationship) VALUES('go:a.go#A','go:a.go#B','implements')`); err != nil {
		t.Fatal(err)
	}
	if err := m.InsertTypeHierarchy(&TypeHierarchy{ChildID: "go:a.go#A", ParentID: "go:a.go#B", Relationship: "implements", Evidence: "tree-sitter-hierarchy"}); err != nil {
		t.Fatal(err)
	}
	result, err := m.GetImplementationsByName("B", nil)
	if err != nil || len(result) != 1 || result[0].RelationshipEvidence != "unknown,tree-sitter-hierarchy" {
		t.Fatalf("hierarchy evidence=%+v error=%v", result, err)
	}
	snapshot, err := m.Snapshot()
	if err != nil || snapshot != nil {
		t.Fatalf("schema extension fabricated a publication: %+v %v", snapshot, err)
	}
}

func seedQualityForInvalidation(t *testing.T) (*Manager, *Snapshot, *BuildAttempt) {
	t.Helper()
	m := newTestManager(t)
	a := seedSymbol(t, m, "go:a.go#A", "A", "/a.go", 1)
	b := seedSymbol(t, m, "go:a.go#B", "B", "/a.go", 2)
	seedSymbol(t, m, "go:a.go#Loose", "Loose", "/a.go", 4)
	if err := m.InsertCall(&Call{CallerID: a.ID, CalleeID: b.ID, File: a.File, Line: 3, Evidence: "lsp-call-hierarchy"}); err != nil {
		t.Fatal(err)
	}
	if err := m.InsertTypeHierarchy(&TypeHierarchy{ChildID: a.ID, ParentID: b.ID, Relationship: "implements", Evidence: "lsp-type-hierarchy"}); err != nil {
		t.Fatal(err)
	}
	if err := m.InsertUnresolved(&Unresolved{Kind: "call", Language: "go", FromID: a.ID, TargetName: "Unknown", File: a.File, Line: 5, Reason: "unresolved"}); err != nil {
		t.Fatal(err)
	}
	if err := m.RecordFileMeta(FileMeta{Path: a.File, ModTime: time.Now(), Language: "go", ContentHash: "hash", Provider: "lsp", ExtractedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	attempt := NewBuildAttempt()
	plan := &RefreshPlan{Attempt: attempt, ProviderState: map[string]string{"go": "provider-digest"}, Phases: []PhaseOutcome{{Language: "go", Phase: "symbols", Provider: "lsp", Status: "complete"}}}
	if err := m.Publish(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	return m, plan.PublishedSnapshot, attempt
}

func TestOlderWriteStatementsInvalidateReports(t *testing.T) {
	statements := []struct{ name, sql string }{
		{"symbol insert", `INSERT INTO symbols(id,name,kind,file,line,column,language) VALUES('new','New','function','/new.go',1,0,'go')`},
		{"symbol update", `UPDATE symbols SET name='Renamed' WHERE name='Loose'`},
		{"symbol delete", `DELETE FROM symbols WHERE name='Loose'`},
		{"call insert", `INSERT OR IGNORE INTO calls(caller_id,callee_id,file,line,column) VALUES('go:a.go#A','go:a.go#B','/a.go',8,0)`},
		{"call update", `UPDATE calls SET line=9`}, {"call delete", `DELETE FROM calls`},
		{"hierarchy insert", `INSERT INTO type_hierarchy(child_id,parent_id,relationship) VALUES('go:a.go#A','go:a.go#B','implements')`},
		{"hierarchy update", `UPDATE type_hierarchy SET relationship='extends'`}, {"hierarchy delete", `DELETE FROM type_hierarchy`},
		{"unresolved insert", `INSERT INTO unresolved_relationships(kind,language,target_name,file,line,column,reason) VALUES('call','go','Missing','/a.go',10,0,'unresolved')`},
		{"unresolved update", `UPDATE unresolved_relationships SET target_name='Other'`}, {"unresolved delete", `DELETE FROM unresolved_relationships`},
		{"manifest insert", `INSERT OR REPLACE INTO file_meta(path,mod_time,language,content_hash,provider,extracted_at) VALUES('/b.go',CURRENT_TIMESTAMP,'go','h','tree-sitter',CURRENT_TIMESTAMP)`},
		{"manifest update", `UPDATE file_meta SET content_hash='different'`}, {"manifest delete", `DELETE FROM file_meta`},
		{"provider insert", `INSERT OR REPLACE INTO provider_state(language,digest) VALUES('go','older-writer')`},
		{"provider update", `UPDATE provider_state SET digest='different'`}, {"provider delete", `DELETE FROM provider_state`},
	}
	for _, test := range statements {
		t.Run(test.name, func(t *testing.T) {
			m, before, _ := seedQualityForInvalidation(t)
			// A second connection models an unaware writer. Trigger maintenance cannot
			// depend on code in the newer Manager or connection-local TEMP triggers.
			older, err := NewManager(m.dbPath)
			if err != nil {
				t.Fatal(err)
			}
			defer older.Close()
			if _, err := older.db.Exec(test.sql); err != nil {
				t.Fatal(err)
			}
			snapshot, err := m.Snapshot()
			if err != nil || snapshot != nil {
				t.Fatalf("stale snapshot exposed: %+v %v", snapshot, err)
			}
			attempt, err := m.LatestAttempt()
			if err != nil || attempt != nil {
				t.Fatalf("stale latest attempt exposed: %+v %v", attempt, err)
			}
			var retained string
			if err := m.db.QueryRow(`SELECT data FROM current_snapshot WHERE singleton=1`).Scan(&retained); err != nil || !bytes.Contains([]byte(retained), []byte(before.ID)) {
				t.Fatalf("invalidation erased history: %v", err)
			}
			if _, err := m.GetStats(); err != nil {
				t.Fatalf("invalidation blocked graph queries: %v", err)
			}
		})
	}
}

func TestConcurrentOlderWriteCannotBeRevalidated(t *testing.T) {
	for _, invalidAtStart := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid start", true: "already invalid start"}[invalidAtStart], func(t *testing.T) {
			m, _, _ := seedQualityForInvalidation(t)
			if invalidAtStart {
				if _, err := m.db.Exec(`UPDATE provider_state SET digest = 'first older write'`); err != nil {
					t.Fatal(err)
				}
			}
			revision, err := m.PublicationRevision()
			if err != nil {
				t.Fatal(err)
			}
			older, err := NewManager(m.dbPath)
			if err != nil {
				t.Fatal(err)
			}
			defer older.Close()
			if _, err := older.db.Exec(`UPDATE symbols SET name = 'Concurrent' WHERE name = 'Loose'`); err != nil {
				t.Fatal(err)
			}
			plan := &RefreshPlan{ExpectedRevision: &revision, Attempt: NewBuildAttempt(), Phases: []PhaseOutcome{{Language: "go", Phase: "symbols", Provider: "lsp", Status: "complete"}}}
			if err := m.Publish(context.Background(), plan); err == nil {
				t.Fatal("stale candidate revalidated an older writer's changed graph")
			}
			snapshot, err := m.Snapshot()
			if err != nil || snapshot != nil {
				t.Fatalf("stale metadata trusted: %+v %v", snapshot, err)
			}
			symbols, err := m.GetSymbolByName("Concurrent", nil)
			if err != nil || len(symbols) != 1 {
				t.Fatalf("older writer's committed graph lost: %+v %v", symbols, err)
			}
		})
	}
}

func TestOlderFailedTransactionRollsBackInvalidation(t *testing.T) {
	m, before, attempt := seedQualityForInvalidation(t)
	revision, err := m.PublicationRevision()
	if err != nil {
		t.Fatal(err)
	}
	tx, err := m.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`DELETE FROM calls`); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO calls(caller_id,callee_id,file,line,column) VALUES('absent','absent','/a.go',1,0)`); err == nil {
		tx.Rollback()
		t.Fatal("expected foreign-key failure")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	after, err := m.Snapshot()
	if err != nil || after == nil || after.ID != before.ID {
		t.Fatalf("failed old write invalidated publication: %+v %v", after, err)
	}
	afterRevision, err := m.PublicationRevision()
	if err != nil || afterRevision != revision {
		t.Fatalf("failed transaction advanced publication revision: before=%d after=%d error=%v", revision, afterRevision, err)
	}
	latest, err := m.LatestAttempt()
	if err != nil || latest == nil || latest.ID != attempt.ID {
		t.Fatalf("failed old write invalidated latest attempt: %+v %v", latest, err)
	}
}
