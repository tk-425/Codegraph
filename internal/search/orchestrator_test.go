package search

import (
	"context"
	"errors"
	"testing"
)

type scriptedTier struct {
	name    string
	results []SearchResult
	err     error
}

func (s scriptedTier) Name() string { return s.name }
func (s scriptedTier) Search(context.Context, SearchOptions) ([]SearchResult, error) {
	return s.results, s.err
}

func TestSearchReportsEveryTierFailing(t *testing.T) {
	o := NewOrchestrator(scriptedTier{name: "database", err: errors.New("corrupt")}, scriptedTier{name: "ripgrep", err: errors.New("missing rg")})
	results, failures, err := o.Search(context.Background(), SearchOptions{Query: "x"})
	if err == nil {
		t.Fatalf("T30: all tiers failed but search returned %d results and no error", len(results))
	}
	if len(failures) != 2 {
		t.Errorf("failures = %+v, want both tiers", failures)
	}
}

func TestSearchDoesNotReportEmptySuccessAfterTierFailure(t *testing.T) {
	o := NewOrchestrator(scriptedTier{name: "database", err: errors.New("corrupt")}, scriptedTier{name: "ripgrep"})
	if _, _, err := o.Search(context.Background(), SearchOptions{Query: "x"}); err == nil {
		t.Fatal("T35: a failed tier followed by no matches was reported as an empty success")
	}
}

func TestSearchReturnsResultsWithEarlierTierFailure(t *testing.T) {
	want := SearchResult{Name: "x", Source: "ripgrep"}
	o := NewOrchestrator(scriptedTier{name: "database", err: errors.New("corrupt")}, scriptedTier{name: "ripgrep", results: []SearchResult{want}})
	results, failures, err := o.Search(context.Background(), SearchOptions{Query: "x"})
	if err != nil || len(results) != 1 || results[0] != want {
		t.Fatalf("results=%+v err=%v, want the later tier's match", results, err)
	}
	if len(failures) != 1 || failures[0].Tier != "database" {
		t.Errorf("failures = %+v, want the database failure kept visible", failures)
	}
}

func TestSearchValidEmptyResultIsSuccess(t *testing.T) {
	o := NewOrchestrator(scriptedTier{name: "database"}, scriptedTier{name: "ripgrep"})
	results, failures, err := o.Search(context.Background(), SearchOptions{Query: "x"})
	if err != nil || len(results) != 0 || len(failures) != 0 {
		t.Fatalf("results=%+v failures=%+v err=%v, want a clean empty success", results, failures, err)
	}
}

func TestSearchStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	o := NewOrchestrator(scriptedTier{name: "database"})
	if _, _, err := o.Search(ctx, SearchOptions{Query: "x"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
