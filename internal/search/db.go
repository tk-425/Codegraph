package search

import (
	"context"

	"github.com/tk-425/Codegraph/internal/db"
)

// DatabaseTier searches the symbol database
type DatabaseTier struct {
	db *db.Manager
}

// NewDatabaseTier creates a new database search tier
func NewDatabaseTier(dbManager *db.Manager) *DatabaseTier {
	return &DatabaseTier{db: dbManager}
}

// Name returns the tier name
func (d *DatabaseTier) Name() string {
	return "database"
}

// Search searches the database for symbols
func (d *DatabaseTier) Search(ctx context.Context, opts SearchOptions) ([]SearchResult, error) {
	var symbols []db.Symbol
	var err error

	if opts.ExactMatch {
		symbols, err = d.db.GetSymbolByName(opts.Query, opts.Languages)
	} else {
		symbols, err = d.db.SearchSymbols(opts.Query, opts.Kind, opts.Languages)
	}

	if err != nil {
		return nil, err
	}

	results := make([]SearchResult, 0, len(symbols))
	for _, sym := range symbols {
		results = append(results, SearchResult{
			Name:      sym.Name,
			Kind:      sym.Kind,
			File:      sym.File,
			Line:      sym.Line,
			Column:    sym.Column,
			Signature: sym.Signature,
			Language:  sym.Language,
			Source:    "db",
			Score:     1.0,
		})
	}

	return results, nil
}
