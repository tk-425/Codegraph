package indexer

import (
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/c"
	"github.com/smacker/go-tree-sitter/cpp"
	"github.com/smacker/go-tree-sitter/csharp"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/ocaml"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/swift"
	"github.com/smacker/go-tree-sitter/typescript/tsx"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
	"github.com/tk-425/Codegraph/internal/db"
)

// grammar is the tree-sitter grammar for a language, or nil.
func grammar(language string) *sitter.Language {
	switch language {
	case "go":
		return golang.GetLanguage()
	case "python":
		return python.GetLanguage()
	case "typescript", "javascript":
		return typescript.GetLanguage()
	case "typescriptreact":
		return tsx.GetLanguage()
	case "java":
		return java.GetLanguage()
	case "swift":
		return swift.GetLanguage()
	case "rust":
		return rust.GetLanguage()
	case "ocaml":
		return ocaml.GetLanguage()
	case "c":
		return c.GetLanguage()
	case "cpp":
		return cpp.GetLanguage()
	case "csharp":
		return csharp.GetLanguage()
	default:
		return nil
	}
}

// walk visits node and its named descendants in source order.
func walk(node *sitter.Node, visit func(*sitter.Node)) {
	visit(node)
	for i := 0; i < int(node.NamedChildCount()); i++ {
		walk(node.NamedChild(i), visit)
	}
}

// nodePosition is the stored position of a node's start: 1-based line,
// UTF-8 byte column (tree-sitter columns are bytes).
func nodePosition(node *sitter.Node) (line, column int) {
	return int(node.StartPoint().Row) + 1, int(node.StartPoint().Column)
}

var (
	// callableKinds can make and receive calls.
	callableKinds = map[string]bool{"function": true, "method": true, "constructor": true}
	// typeKinds can take part in a type relationship.
	typeKinds = map[string]bool{"class": true, "interface": true, "struct": true, "type": true, "enum": true, "module": true}
)

// fileSymbols attaches positions in one file to the symbols indexed there.
// Relationships are attached through it, never by reconstructing an ID.
type fileSymbols []db.Symbol

func loadFileSymbols(store graphStore, path string) (fileSymbols, error) {
	symbols, err := store.GetFileSymbols(path)
	if err != nil {
		return nil, err
	}
	return fileSymbols(symbols), nil
}

// named returns the symbol whose name starts exactly at line:column.
func (f fileSymbols) named(line, column int) *db.Symbol {
	for i := range f {
		if f[i].Line == line && f[i].Column == column {
			return &f[i]
		}
	}
	return nil
}

// enclosing returns the innermost symbol of one of kinds whose declaration
// range contains line:column, comparing columns as well as lines.
func (f fileSymbols) enclosing(line, column int, kinds map[string]bool) *db.Symbol {
	var best *db.Symbol
	for i := range f {
		s := &f[i]
		if !kinds[s.Kind] || s.EndLine == nil || s.EndColumn == nil {
			continue
		}
		if before(line, column, s.StartLine, s.StartColumn) || !before(line, column, *s.EndLine, *s.EndColumn) {
			continue
		}
		if best == nil || before(best.StartLine, best.StartColumn, s.StartLine, s.StartColumn) {
			best = s
		}
	}
	return best
}

// before reports whether position a is strictly before position b.
func before(aLine, aColumn, bLine, bColumn int) bool {
	return aLine < bLine || (aLine == bLine && aColumn < bColumn)
}
