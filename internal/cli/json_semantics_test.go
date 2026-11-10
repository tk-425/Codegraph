package cli

import (
	"encoding/json"
	"testing"

	"github.com/tk-425/Codegraph/internal/db"
)

// T18: one site dispatching to an interface method and its implementation
// lists both targets under callees and its caller once under callers; a
// repeated insert of the same call is not a second call.
func TestJSONSemantics_DistinctTargetsAtOneSite(t *testing.T) {
	_, m := setupCodegraphProject(t)
	dispatch := db.Symbol{
		ID: "go:dispatch.go#Dispatch", Name: "Dispatch", Kind: "function",
		File: "dispatch.go", Line: 9, Language: "go",
	}
	runnerRun := db.Symbol{
		ID: "go:dispatch.go#Runner.Run", Name: "Run", Kind: "method", Scope: "Runner",
		File: "dispatch.go", Line: 3, Language: "go",
	}
	jobRun := db.Symbol{
		ID: "go:dispatch.go#Job.Run", Name: "Run", Kind: "method", Scope: "Job",
		File: "dispatch.go", Line: 7, Language: "go",
	}
	for _, s := range []db.Symbol{dispatch, runnerRun, jobRun} {
		seedSymbol(t, m, s)
	}
	for _, callee := range []db.Symbol{runnerRun, jobRun, jobRun} {
		if err := m.InsertCall(&db.Call{
			CallerID: dispatch.ID, CalleeID: callee.ID,
			File: "dispatch.go", Line: 10, Column: 3,
		}); err != nil {
			t.Fatalf("InsertCall(%s): %v", callee.ID, err)
		}
	}
	stats, err := m.GetStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.CallCount != 2 {
		t.Errorf("stored %d calls, want 2: a repeated call site is one call", stats.CallCount)
	}

	c, buf := freshCmd(t, "callees", runCallees)
	if err := c.RunE(c, []string{"Dispatch"}); err != nil {
		t.Fatalf("runCallees returned error: %v", err)
	}
	env, count := decodeEnvelope(t, buf.Bytes())
	if count != 2 {
		t.Fatalf("T18: callees count = %d, want 2 (interface + implementation), env=%s", count, buf.String())
	}
	var callees []calleeRecord
	_ = json.Unmarshal(env["results"], &callees)
	for _, rec := range callees {
		if rec.Name != "Run" || rec.Line != 10 {
			t.Errorf("T18: callee record = %+v, want Run at line 10", rec)
		}
	}

	c, buf = freshCmd(t, "callers", runCallers)
	if err := c.RunE(c, []string{"Run"}); err != nil {
		t.Fatalf("runCallers returned error: %v", err)
	}
	if _, count := decodeEnvelope(t, buf.Bytes()); count != 1 {
		t.Errorf("callers count = %d, want Dispatch once, env=%s", count, buf.String())
	}
}

// T17: the stored tier honours --lang and never reports Go embedding as an
// implementation.
func TestJSONSemantics_StoredImplementations(t *testing.T) {
	_, m := setupCodegraphProject(t)
	symbols := []db.Symbol{
		{ID: "python:base.py#Base", Name: "Base", Kind: "class", File: "base.py", Line: 1, Language: "python"},
		{ID: "python:base.py#Derived", Name: "Derived", Kind: "class", File: "base.py", Line: 4, Language: "python"},
		{ID: "typescript:base.ts#Base", Name: "Base", Kind: "class", File: "base.ts", Line: 1, Language: "typescript"},
		{ID: "typescript:base.ts#Child", Name: "Child", Kind: "class", File: "base.ts", Line: 3, Language: "typescript"},
		{ID: "go:base.go#Base", Name: "Base", Kind: "struct", File: "base.go", Line: 3, Language: "go"},
		{ID: "go:base.go#Outer", Name: "Outer", Kind: "struct", File: "base.go", Line: 5, Language: "go"},
	}
	for _, s := range symbols {
		seedSymbol(t, m, s)
	}
	for _, rel := range []db.TypeHierarchy{
		{ChildID: "python:base.py#Derived", ParentID: "python:base.py#Base", Relationship: "extends"},
		{ChildID: "typescript:base.ts#Child", ParentID: "typescript:base.ts#Base", Relationship: "extends"},
		{ChildID: "go:base.go#Outer", ParentID: "go:base.go#Base", Relationship: "embeds"},
	} {
		if err := m.InsertTypeHierarchy(&rel); err != nil {
			t.Fatalf("InsertTypeHierarchy(%s): %v", rel.ChildID, err)
		}
	}

	implementations := func(lang string) []implementationRecord {
		t.Helper()
		implementationsLangFlag = lang
		t.Cleanup(func() { implementationsLangFlag = "" })
		c, buf := freshCmd(t, "implementations", runImplementations)
		if err := c.RunE(c, []string{"Base"}); err != nil {
			t.Fatalf("runImplementations returned error: %v", err)
		}
		env, _ := decodeEnvelope(t, buf.Bytes())
		var recs []implementationRecord
		_ = json.Unmarshal(env["results"], &recs)
		return recs
	}

	all := implementations("")
	if len(all) != 2 || all[0].Name != "Derived" || all[1].Name != "Child" {
		t.Errorf("T17: implementations of Base = %+v, want Derived and Child without Go embedding", all)
	}
	python := implementations("python")
	if len(python) != 1 || python[0].Name != "Derived" {
		t.Errorf("T17: --lang python = %+v, want Derived only", python)
	}
}
