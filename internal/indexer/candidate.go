package indexer

import (
	"sort"
	"strings"

	"github.com/tk-425/Codegraph/internal/db"
)

// graphStore reads symbols and receives extraction output for one graph.
// *db.Manager satisfies it against the published graph, which direct users
// and tests rely on; during a refresh the refreshStore below satisfies it
// against the attempt's merged view. Every extractor takes this, so nothing
// outside db writes the published graph.
type graphStore interface {
	// BeginFile starts a fresh provider attempt at a file: output an
	// earlier failed attempt left for it must not survive into this one.
	BeginFile(path string)
	InsertSymbol(*db.Symbol) error
	InsertCall(*db.Call) error
	InsertTypeHierarchy(*db.TypeHierarchy) error
	InsertUnresolved(*db.Unresolved) error
	RecordFileMeta(meta db.FileMeta) error

	GetFileSymbols(file string) ([]db.Symbol, error)
	GetFunctionSymbols(language string) ([]db.Symbol, error)
	GetTypeSymbols(language string) ([]db.Symbol, error)
	GetSymbolByName(name string, languages []string) ([]db.Symbol, error)
}

// candidate is the graph one build attempt produces. Extraction writes only
// here; the published graph changes only when db.Manager.Publish applies
// the resulting RefreshPlan in one transaction (R05).
type candidate struct {
	// changed marks files this attempt re-extracted. Their published rows
	// are stale for the rest of the attempt: reads must not see them, and
	// publication replaces them.
	changed map[string]bool
	symbols map[string][]*db.Symbol
	meta    map[string]db.FileMeta

	// Edges are the complete replacement sets for the dirty languages.
	calls      []*db.Call
	hierarchy  []*db.TypeHierarchy
	unresolved []*db.Unresolved
}

func newCandidate() *candidate {
	return &candidate{
		changed: map[string]bool{},
		symbols: map[string][]*db.Symbol{},
		meta:    map[string]db.FileMeta{},
	}
}

// beginFile drops anything an earlier, failed attempt at this file left in
// the candidate, so a provider fallback replaces instead of appending.
func (c *candidate) beginFile(path string) {
	c.changed[path] = true
	c.symbols[path] = nil
}

func (c *candidate) InsertSymbol(s *db.Symbol) error {
	c.symbols[s.File] = append(c.symbols[s.File], s)
	return nil
}

func (c *candidate) InsertCall(call *db.Call) error {
	c.calls = append(c.calls, call)
	return nil
}

func (c *candidate) InsertTypeHierarchy(th *db.TypeHierarchy) error {
	c.hierarchy = append(c.hierarchy, th)
	return nil
}

func (c *candidate) InsertUnresolved(u *db.Unresolved) error {
	c.unresolved = append(c.unresolved, u)
	return nil
}

func (c *candidate) RecordFileMeta(meta db.FileMeta) error {
	c.meta[meta.Path] = meta
	return nil
}

// plan freezes the candidate into what publication applies: the files it
// re-extracted replace their published symbols, removed files disappear,
// and the dirty languages' edge sets are the candidate's edges.
func (c *candidate) plan(removed, dirty []string) *db.RefreshPlan {
	plan := &db.RefreshPlan{
		RemovedFiles:   removed,
		DirtyLanguages: dirty,
		Calls:          c.calls,
		TypeHierarchy:  c.hierarchy,
		Unresolved:     c.unresolved,
	}
	for path, symbols := range c.symbols {
		plan.ChangedFiles = append(plan.ChangedFiles, path)
		for _, s := range symbols {
			plan.Symbols = append(plan.Symbols, s)
		}
	}
	sort.Strings(plan.ChangedFiles)
	sort.Slice(plan.Symbols, func(i, j int) bool {
		if plan.Symbols[i].File != plan.Symbols[j].File {
			return plan.Symbols[i].File < plan.Symbols[j].File
		}
		if plan.Symbols[i].StartLine != plan.Symbols[j].StartLine {
			return plan.Symbols[i].StartLine < plan.Symbols[j].StartLine
		}
		return plan.Symbols[i].StartColumn < plan.Symbols[j].StartColumn
	})
	for _, meta := range c.meta {
		plan.FileMeta = append(plan.FileMeta, meta)
	}
	sort.Slice(plan.FileMeta, func(i, j int) bool { return plan.FileMeta[i].Path < plan.FileMeta[j].Path })
	return plan
}

// refreshStore is the store extraction works against during one attempt:
// writes land in the candidate, reads see the candidate's symbols for the
// files it re-extracted merged with the published rows of every other file.
type refreshStore struct {
	published *db.Manager
	candidate *candidate
}

func (s *refreshStore) BeginFile(path string)             { s.candidate.beginFile(path) }
func (s *refreshStore) InsertSymbol(sym *db.Symbol) error { return s.candidate.InsertSymbol(sym) }
func (s *refreshStore) InsertCall(c *db.Call) error       { return s.candidate.InsertCall(c) }
func (s *refreshStore) InsertTypeHierarchy(t *db.TypeHierarchy) error {
	return s.candidate.InsertTypeHierarchy(t)
}
func (s *refreshStore) InsertUnresolved(u *db.Unresolved) error {
	return s.candidate.InsertUnresolved(u)
}
func (s *refreshStore) RecordFileMeta(meta db.FileMeta) error {
	return s.candidate.RecordFileMeta(meta)
}

func (s *refreshStore) GetFileSymbols(file string) ([]db.Symbol, error) {
	if s.candidate.changed[file] {
		return copySymbols(s.candidate.symbols[file]), nil
	}
	return s.published.GetFileSymbols(file)
}

func (s *refreshStore) GetFunctionSymbols(language string) ([]db.Symbol, error) {
	return s.mergeByLanguage(language, callableKinds, s.published.GetFunctionSymbols)
}

func (s *refreshStore) GetTypeSymbols(language string) ([]db.Symbol, error) {
	return s.mergeByLanguage(language, typeKinds, s.published.GetTypeSymbols)
}

// mergeByLanguage joins the published rows of the files this attempt is not
// re-extracting with the candidate's matching rows, ordered like the
// published query.
func (s *refreshStore) mergeByLanguage(language string, kinds map[string]bool, published func(string) ([]db.Symbol, error)) ([]db.Symbol, error) {
	rows, err := published(language)
	if err != nil {
		return nil, err
	}
	merged := make([]db.Symbol, 0, len(rows))
	for _, sym := range rows {
		if !s.candidate.changed[sym.File] {
			merged = append(merged, sym)
		}
	}
	for _, symbols := range s.candidate.symbols {
		for _, sym := range symbols {
			if sym.Language == language && kinds[sym.Kind] {
				merged = append(merged, *sym)
			}
		}
	}
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].File != merged[j].File {
			return merged[i].File < merged[j].File
		}
		if merged[i].Line != merged[j].Line {
			return merged[i].Line < merged[j].Line
		}
		return merged[i].ID < merged[j].ID
	})
	return merged, nil
}

// GetSymbolByName answers with the published rows of unchanged files plus
// the candidate's rows matching the same three name shapes the published
// query matches (exact, with parameters, qualified with parameters).
func (s *refreshStore) GetSymbolByName(name string, languages []string) ([]db.Symbol, error) {
	rows, err := s.published.GetSymbolByName(name, languages)
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, language := range languages {
		allowed[language] = true
	}
	merged := make([]db.Symbol, 0, len(rows))
	for _, sym := range rows {
		if !s.candidate.changed[sym.File] {
			merged = append(merged, sym)
		}
	}
	for _, symbols := range s.candidate.symbols {
		for _, sym := range symbols {
			if (len(languages) == 0 || allowed[sym.Language]) && matchesNameShapes(sym.Name, name) {
				merged = append(merged, *sym)
			}
		}
	}
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].File != merged[j].File {
			return merged[i].File < merged[j].File
		}
		return merged[i].Line < merged[j].Line
	})
	return merged, nil
}

// matchesNameShapes mirrors the name matching of the published query in
// db.Manager.GetSymbolByName: the exact name, the name with parameters, or
// a qualified name with parameters. Keep the two in step.
func matchesNameShapes(symbolName, name string) bool {
	return symbolName == name ||
		strings.HasPrefix(symbolName, name+"(") ||
		strings.Contains(symbolName, "."+name+"(")
}

func copySymbols(symbols []*db.Symbol) []db.Symbol {
	out := make([]db.Symbol, 0, len(symbols))
	for _, sym := range symbols {
		out = append(out, *sym)
	}
	return out
}
