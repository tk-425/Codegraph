package search

import (
	"context"
	"errors"
	"fmt"
)

// SearchResult represents a search match
type SearchResult struct {
	ID        string  `json:"id,omitempty"`
	Name      string  `json:"name"`
	Kind      string  `json:"kind"`
	File      string  `json:"file"`
	Line      int     `json:"line"`
	Column    int     `json:"column"`
	Signature string  `json:"signature,omitempty"`
	Language  string  `json:"language"`
	Source    string  `json:"source"` // Extraction provider: lsp, tree-sitter or ripgrep
	Score     float64 `json:"score"`
	Context   string  `json:"context,omitempty"` // Line content for ripgrep results
}

// SearchOptions configures search behavior
type SearchOptions struct {
	Query      string
	Kind       string   // Optional: filter by kind (function, class, etc.)
	Languages  []string // Optional: filter by language
	Limit      int      // Max results (0 = unlimited)
	ExactMatch bool     // Require exact name match
}

// Tier represents a search tier in the fallback chain
type Tier interface {
	Name() string
	Search(ctx context.Context, opts SearchOptions) ([]SearchResult, error)
}

// Orchestrator coordinates multi-tier search
type Orchestrator struct {
	tiers []Tier
}

// NewOrchestrator creates a new search orchestrator
func NewOrchestrator(tiers ...Tier) *Orchestrator {
	return &Orchestrator{tiers: tiers}
}

// TierFailure records a tier that could not answer a search.
type TierFailure struct {
	Tier string
	Err  error
}

// Search executes search across all tiers until results are found. Tier
// failures are returned rather than printed; when no tier produced results
// and any tier failed, the empty result is not reported as a success.
func (o *Orchestrator) Search(ctx context.Context, opts SearchOptions) ([]SearchResult, []TierFailure, error) {
	var failures []TierFailure
	for _, tier := range o.tiers {
		if err := ctx.Err(); err != nil {
			return nil, failures, err
		}
		results, err := tier.Search(ctx, opts)
		if err != nil {
			failures = append(failures, TierFailure{Tier: tier.Name(), Err: err})
			continue
		}

		if len(results) > 0 {
			// Apply limit if specified
			if opts.Limit > 0 && len(results) > opts.Limit {
				results = results[:opts.Limit]
			}
			return results, failures, nil
		}
	}

	if len(failures) > 0 {
		errs := make([]error, 0, len(failures))
		for _, failure := range failures {
			errs = append(errs, fmt.Errorf("%s tier: %w", failure.Tier, failure.Err))
		}
		return nil, failures, errors.Join(errs...)
	}
	return []SearchResult{}, nil, nil
}

// SearchAll executes search across all tiers and merges results
func (o *Orchestrator) SearchAll(ctx context.Context, opts SearchOptions) ([]SearchResult, error) {
	var allResults []SearchResult
	seen := make(map[string]bool)

	for _, tier := range o.tiers {
		results, err := tier.Search(ctx, opts)
		if err != nil {
			continue
		}

		for _, r := range results {
			key := fmt.Sprintf("%s:%s:%d", r.File, r.Name, r.Line)
			if !seen[key] {
				seen[key] = true
				allResults = append(allResults, r)
			}
		}
	}

	// Apply limit if specified
	if opts.Limit > 0 && len(allResults) > opts.Limit {
		allResults = allResults[:opts.Limit]
	}

	return allResults, nil
}
