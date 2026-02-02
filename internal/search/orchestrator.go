package search

import (
	"context"
	"fmt"
)

// SearchResult represents a search match
type SearchResult struct {
	Name       string  `json:"name"`
	Kind       string  `json:"kind"`
	File       string  `json:"file"`
	Line       int     `json:"line"`
	Column     int     `json:"column"`
	Signature  string  `json:"signature,omitempty"`
	Language   string  `json:"language"`
	Source     string  `json:"source"` // "db", "treesitter", "ripgrep"
	Score      float64 `json:"score"`
	Context    string  `json:"context,omitempty"` // Line content for ripgrep results
}

// SearchOptions configures search behavior
type SearchOptions struct {
	Query     string
	Kind      string   // Optional: filter by kind (function, class, etc.)
	Languages []string // Optional: filter by language
	Limit     int      // Max results (0 = unlimited)
	ExactMatch bool    // Require exact name match
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

// Search executes search across all tiers until results are found
func (o *Orchestrator) Search(ctx context.Context, opts SearchOptions) ([]SearchResult, error) {
	for _, tier := range o.tiers {
		results, err := tier.Search(ctx, opts)
		if err != nil {
			// Log error but continue to next tier
			fmt.Printf("   ⚠️  %s tier error: %v\n", tier.Name(), err)
			continue
		}

		if len(results) > 0 {
			// Apply limit if specified
			if opts.Limit > 0 && len(results) > opts.Limit {
				results = results[:opts.Limit]
			}
			return results, nil
		}
	}

	return []SearchResult{}, nil
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
