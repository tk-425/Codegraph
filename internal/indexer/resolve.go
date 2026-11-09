package indexer

import (
	"path/filepath"

	"github.com/tk-425/Codegraph/internal/db"
)

// Reasons an observed relationship site is recorded instead of resolved.
const (
	reasonUnresolved  = "unresolved"
	reasonAmbiguous   = "ambiguous"
	reasonExternal    = "external"
	reasonUnvalidated = "unvalidated"
)

// languageFamily is the set of languages whose symbols one file may bind to.
// TypeScript, TSX and JavaScript import each other; nothing else crosses.
func languageFamily(language string) []string {
	switch language {
	case "typescript", "typescriptreact", "javascript":
		return []string{"typescript", "typescriptreact", "javascript"}
	default:
		return []string{language}
	}
}

// evidence is what is known about the site an observed name came from.
type evidence struct {
	language string
	file     string
	// owner is the scope the target must be declared in, when the site
	// proves it (self.m(), this.m(), or a Go receiver variable); empty
	// otherwise.
	owner string
}

// resolution is the outcome of resolving an observed name.
type resolution struct {
	symbol     *db.Symbol
	reason     string // empty when resolved
	candidates int
}

// resolveTarget picks the local symbol an observed name refers to, using
// the narrowest evidence available. A proven owner is the only tier when
// present (an inherited member stays unresolved rather than binding
// elsewhere); otherwise the same file, then (Go) the same package directory,
// then the language family. The first tier with candidates decides: one is
// resolved, several are ambiguous. It never takes the first of several and
// never crosses language families.
func resolveTarget(database *db.Manager, name string, kinds map[string]bool, at evidence) (resolution, error) {
	found, err := database.GetSymbolByName(name, languageFamily(at.language))
	if err != nil {
		return resolution{}, err
	}
	var candidates []db.Symbol
	for _, s := range found {
		if kinds[s.Kind] {
			candidates = append(candidates, s)
		}
	}
	tiers := []func(db.Symbol) bool{
		func(s db.Symbol) bool { return s.File == at.file },
		func(s db.Symbol) bool {
			return at.language == "go" && filepath.Dir(s.File) == filepath.Dir(at.file)
		},
		func(db.Symbol) bool { return true },
	}
	if at.owner != "" {
		tiers = []func(db.Symbol) bool{func(s db.Symbol) bool { return s.Scope == at.owner }}
	}
	for _, inTier := range tiers {
		var matched []db.Symbol
		for _, s := range candidates {
			if inTier(s) {
				matched = append(matched, s)
			}
		}
		switch len(matched) {
		case 0:
			continue
		case 1:
			return resolution{symbol: &matched[0], candidates: 1}, nil
		default:
			return resolution{reason: reasonAmbiguous, candidates: len(matched)}, nil
		}
	}
	return resolution{reason: reasonUnresolved}, nil
}
