package indexer

import (
	"context"
	"fmt"
	"os"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/tk-425/Codegraph/internal/db"
)

// CallExtractor extracts call relationships using tree-sitter
type CallExtractor struct {
	db       *db.Manager
	rootPath string
}

// NewCallExtractor creates a new call extractor
func NewCallExtractor(dbManager *db.Manager, rootPath string) *CallExtractor {
	return &CallExtractor{
		db:       dbManager,
		rootPath: rootPath,
	}
}

// callSyntax is how one language spells an invocation: its call node types,
// and where the invoked name (and, for obj.m(), the object) sits in one.
type callSyntax struct {
	calls  map[string]bool
	callee func(call *sitter.Node) (name, object *sitter.Node)
}

// isCall reports whether n is an invocation node of this language.
func (s callSyntax) isCall(n *sitter.Node) bool { return s.calls[n.Type()] }

// memberCallee reads a call whose function field is either a plain
// identifier or a member access with the given name and object fields.
func memberCallee(identifier, member, nameField, objectField string) func(*sitter.Node) (*sitter.Node, *sitter.Node) {
	return func(call *sitter.Node) (*sitter.Node, *sitter.Node) {
		fn := call.ChildByFieldName("function")
		if fn == nil && call.NamedChildCount() > 0 {
			fn = call.NamedChild(0)
		}
		if fn == nil {
			return nil, nil
		}
		switch fn.Type() {
		case identifier:
			return fn, nil
		case member:
			return fn.ChildByFieldName(nameField), fn.ChildByFieldName(objectField)
		}
		return nil, nil
	}
}

func lastNamedChild(n *sitter.Node) *sitter.Node {
	if n.NamedChildCount() == 0 {
		return n
	}
	return n.NamedChild(int(n.NamedChildCount()) - 1)
}

var callSyntaxes = map[string]callSyntax{
	"go": {
		calls:  map[string]bool{"call_expression": true},
		callee: memberCallee("identifier", "selector_expression", "field", "operand"),
	},
	"python": {
		calls:  map[string]bool{"call": true},
		callee: memberCallee("identifier", "attribute", "attribute", "object"),
	},
	"typescript": {
		calls:  map[string]bool{"call_expression": true},
		callee: memberCallee("identifier", "member_expression", "property", "object"),
	},
	"java": {
		calls: map[string]bool{"method_invocation": true},
		callee: func(call *sitter.Node) (*sitter.Node, *sitter.Node) {
			return call.ChildByFieldName("name"), call.ChildByFieldName("object")
		},
	},
	"csharp": {
		calls: map[string]bool{"invocation_expression": true},
		callee: func(call *sitter.Node) (*sitter.Node, *sitter.Node) {
			for i := 0; i < int(call.NamedChildCount()); i++ {
				child := call.NamedChild(i)
				switch child.Type() {
				case "member_access_expression":
					return child.ChildByFieldName("name"), child.ChildByFieldName("expression")
				case "identifier":
					return child, nil
				}
			}
			return nil, nil
		},
	},
	"rust": {
		calls: map[string]bool{"call_expression": true},
		callee: func(call *sitter.Node) (*sitter.Node, *sitter.Node) {
			fn := call.ChildByFieldName("function")
			if fn == nil {
				return nil, nil
			}
			switch fn.Type() {
			case "identifier":
				return fn, nil
			case "field_expression":
				return fn.ChildByFieldName("field"), fn.ChildByFieldName("value")
			case "scoped_identifier":
				return lastNamedChild(fn), nil
			}
			return nil, nil
		},
	},
	"swift": {
		calls: map[string]bool{"call_expression": true},
		callee: func(call *sitter.Node) (*sitter.Node, *sitter.Node) {
			if call.NamedChildCount() == 0 {
				return nil, nil
			}
			fn := call.NamedChild(0)
			switch fn.Type() {
			case "simple_identifier":
				return fn, nil
			case "navigation_expression":
				suffix := fn.ChildByFieldName("suffix")
				if suffix != nil && suffix.Type() == "navigation_suffix" {
					suffix = lastNamedChild(suffix)
				}
				return suffix, fn.ChildByFieldName("target")
			}
			return nil, nil
		},
	},
	"ocaml": {
		calls: map[string]bool{"application_expression": true},
		callee: func(call *sitter.Node) (*sitter.Node, *sitter.Node) {
			fn := call.ChildByFieldName("function")
			if fn == nil && call.NamedChildCount() > 0 {
				fn = call.NamedChild(0)
			}
			if fn == nil {
				return nil, nil
			}
			switch fn.Type() {
			case "value_path", "constructor_path":
				return lastNamedChild(fn), nil
			case "field_get_expression":
				return fn.ChildByFieldName("field"), nil
			}
			return fn, nil
		},
	},
}

func init() {
	callSyntaxes["javascript"] = callSyntaxes["typescript"]
	callSyntaxes["typescriptreact"] = callSyntaxes["typescript"]
}

// ExtractCalls extracts call relationships from a file using tree-sitter.
// The caller is the innermost indexed callable containing the invoked name;
// the call site is that name's position.
func (c *CallExtractor) ExtractCalls(ctx context.Context, file FileInfo) (int, error) {
	syntax, ok := callSyntaxes[file.Language]
	lang := grammar(file.Language)
	if !ok || lang == nil {
		return 0, nil // Language not supported
	}

	content, err := os.ReadFile(file.Path)
	if err != nil {
		return 0, fmt.Errorf("failed to read file: %w", err)
	}

	parser := sitter.NewParser()
	parser.SetLanguage(lang)

	tree, err := parser.ParseCtx(ctx, nil, content)
	if err != nil {
		return 0, fmt.Errorf("tree-sitter parse error: %w", err)
	}
	defer tree.Close()

	symbols, err := loadFileSymbols(c.db, file.Path)
	if err != nil {
		return 0, fmt.Errorf("failed to load file symbols: %w", err)
	}

	var calls []*db.Call
	walk(tree.RootNode(), func(n *sitter.Node) {
		if !syntax.isCall(n) {
			return
		}
		name, _ := syntax.callee(n)
		if name == nil {
			return
		}
		line, column := nodePosition(name)
		caller := symbols.enclosing(line, column, callableKinds)
		if caller == nil {
			return
		}
		calleeID := c.resolveSymbolID(name.Content(content), file.Language)
		if calleeID == "" {
			return
		}
		calls = append(calls, &db.Call{
			CallerID: caller.ID,
			CalleeID: calleeID,
			File:     file.Path,
			Line:     line,
			Column:   column,
		})
	})

	count := 0
	for _, call := range calls {
		if err := c.db.InsertCall(call); err != nil {
			// Skip duplicates
			continue
		}
		count++
	}

	return count, nil
}

// resolveSymbolID looks up a symbol ID from the database
func (c *CallExtractor) resolveSymbolID(name string, language string) string {
	// Try to find the symbol in the database
	symbols, err := c.db.GetSymbolByName(name, []string{language})
	if err != nil || len(symbols) == 0 {
		// Try without language filter
		symbols, err = c.db.GetSymbolByName(name, nil)
		if err != nil || len(symbols) == 0 {
			return ""
		}
	}
	return symbols[0].ID
}
