package db

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestPublicationQualityRollbackAndNoOp(t *testing.T) {
	m := newTestManager(t)
	attempt := NewBuildAttempt()
	if err := m.RecordAttempt(context.Background(), attempt); err != nil {
		t.Fatal(err)
	}
	plan := &RefreshPlan{Attempt: attempt, Phases: []PhaseOutcome{{Language: "go", Phase: "symbols", Provider: "tree-sitter", Status: "fallback"}}}
	if err := m.Publish(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	first, err := m.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if first == nil || first.Quality != "degraded" || attempt.Outcome != "succeeded" || attempt.SnapshotID != first.ID {
		t.Fatalf("snapshot=%+v attempt=%+v", first, attempt)
	}
	noop := NewBuildAttempt()
	if err := m.Publish(context.Background(), &RefreshPlan{Attempt: noop}); err != nil {
		t.Fatal(err)
	}
	after, err := m.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, after) || noop.Changed || noop.SnapshotID != first.ID {
		t.Fatalf("no-op replaced quality: first=%+v after=%+v attempt=%+v", first, after, noop)
	}
	failure := NewBuildAttempt()
	bad := &RefreshPlan{Attempt: failure, DirtyLanguages: []string{"go"}, Calls: []*Call{{CallerID: "missing", CalleeID: "missing", File: "a.go"}}}
	if err := m.Publish(context.Background(), bad); err == nil {
		t.Fatal("expected FK failure")
	}
	after, err = m.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, after) || failure.Outcome != "running" {
		t.Fatalf("failed publication changed snapshot or attempt: %+v %+v", after, failure)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.Publish(ctx, &RefreshPlan{Attempt: failure}); err == nil {
		t.Fatal("cancelled publication succeeded")
	}
	latest, err := m.LatestAttempt()
	if err != nil {
		t.Fatal(err)
	}
	if latest.ID != noop.ID || latest.Outcome != "succeeded" {
		t.Fatalf("unexpected latest attempt: %+v", latest)
	}
}

func TestRelationshipEvidenceUnionsWithoutDuplicates(t *testing.T) {
	m := newTestManager(t)
	a := seedSymbol(t, m, "go:a.go#A", "A", "/a.go", 1)
	b := seedSymbol(t, m, "go:a.go#B", "B", "/a.go", 2)
	for _, kind := range []string{"lsp-call-hierarchy", "tree-sitter-call", "tree-sitter-call"} {
		if err := m.InsertCall(&Call{CallerID: a.ID, CalleeID: b.ID, File: a.File, Line: 3, Evidence: kind}); err != nil {
			t.Fatal(err)
		}
	}
	for _, kind := range []string{"lsp-type-hierarchy", "tree-sitter-hierarchy", "tree-sitter-hierarchy"} {
		if err := m.InsertTypeHierarchy(&TypeHierarchy{ChildID: a.ID, ParentID: b.ID, Relationship: "implements", Evidence: kind}); err != nil {
			t.Fatal(err)
		}
	}
	callees, err := m.GetCallees("A", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(callees) != 1 || callees[0].Evidence != "lsp-call-hierarchy,tree-sitter-call" {
		t.Fatalf("call evidence=%+v", callees)
	}
	implementations, err := m.GetImplementationsByName("B", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(implementations) != 1 || implementations[0].RelationshipEvidence != "lsp-type-hierarchy,tree-sitter-hierarchy" {
		t.Fatalf("hierarchy evidence=%+v", implementations)
	}
}

func TestIncompatibleReplacementFailurePreservesPublication(t *testing.T) {
	m := newTestManager(t)
	if err := m.Publish(context.Background(), &RefreshPlan{}); err != nil {
		t.Fatal(err)
	}
	before, err := m.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.db.Exec("PRAGMA user_version = 0"); err != nil {
		t.Fatal(err)
	}
	if err := m.CheckFormat(); err == nil {
		t.Fatal("incompatible identity format accepted")
	}
	bad := &RefreshPlan{ResetFormat: true, Calls: []*Call{{CallerID: "absent", CalleeID: "absent", File: "a.go"}}}
	if err := m.Publish(context.Background(), bad); err == nil {
		t.Fatal("expected publication failure")
	}
	after, err := m.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	var version int
	if err := m.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 0 || !reflect.DeepEqual(before, after) {
		t.Fatalf("legacy graph/metadata changed on failure: format=%d snapshot=%+v", version, after)
	}
}

func TestPinnedReadPairsGraphAndPublication(t *testing.T) {
	m := newTestManager(t)
	file := "/project/a.go"
	plan := &RefreshPlan{ChangedFiles: []string{file}, Symbols: []*Symbol{{ID: "go:a.go#Old", Name: "Old", Kind: "function", File: file, Line: 1, Language: "go", CreatedAt: time.Now()}}}
	if err := m.Publish(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	reader, err := OpenReadOnly(m.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.PinRead(context.Background()); err != nil {
		reader.Close()
		t.Fatal(err)
	}
	snapshot, err := reader.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	// A rollback-journal reader blocks the commit; a second reader still sees
	// the complete old state. Release it before waiting for the writer.
	done := make(chan error, 1)
	replacement := &RefreshPlan{ChangedFiles: []string{file}, Symbols: []*Symbol{{ID: "go:a.go#New", Name: "New", Kind: "function", File: file, Line: 1, Language: "go", CreatedAt: time.Now()}}}
	go func() { done <- m.Publish(context.Background(), replacement) }()
	symbols, err := reader.GetSymbolByName("Old", nil)
	if err != nil {
		t.Fatal(err)
	}
	same, err := reader.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(symbols) != 1 || same.ID != snapshot.ID || same.Counts.Symbols != 1 {
		t.Fatalf("incoherent reader: symbols=%+v snapshot=%+v", symbols, same)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	current, err := m.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if current.ID == snapshot.ID {
		t.Fatal("writer failed to advance snapshot")
	}
}
