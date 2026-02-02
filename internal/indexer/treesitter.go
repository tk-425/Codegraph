package indexer

import (
	"context"
	"fmt"
	"os"
	"time"

	sitter "github.com/smacker/go-tree-sitter"
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

// TreeSitterIndexer provides fallback symbol extraction using tree-sitter
type TreeSitterIndexer struct {
	db       *db.Manager
	rootPath string
}

// NewTreeSitterIndexer creates a new tree-sitter based indexer
func NewTreeSitterIndexer(dbManager *db.Manager, rootPath string) *TreeSitterIndexer {
	return &TreeSitterIndexer{
		db:       dbManager,
		rootPath: rootPath,
	}
}

// IndexFile extracts symbols from a file using tree-sitter
func (t *TreeSitterIndexer) IndexFile(ctx context.Context, file FileInfo) (int, error) {
	// Get the appropriate language
	lang := t.getLanguage(file.Language)
	if lang == nil {
		return 0, fmt.Errorf("tree-sitter does not support language: %s", file.Language)
	}

	// Read file content
	content, err := os.ReadFile(file.Path)
	if err != nil {
		return 0, fmt.Errorf("failed to read file: %w", err)
	}

	// Parse using tree-sitter
	parser := sitter.NewParser()
	parser.SetLanguage(lang)

	tree, err := parser.ParseCtx(ctx, nil, content)
	if err != nil {
		return 0, fmt.Errorf("tree-sitter parse error: %w", err)
	}
	defer tree.Close()

	// Extract symbols from the tree
	symbols := t.extractSymbols(tree.RootNode(), content, file, "")

	// Store symbols in database
	for _, sym := range symbols {
		if err := t.db.InsertSymbol(sym); err != nil {
			return 0, err
		}
	}

	// Update file metadata
	if err := t.db.UpdateFileMeta(file.Path, time.Now(), file.Language); err != nil {
		return 0, err
	}

	return len(symbols), nil
}

// getLanguage returns the tree-sitter language for a given language name
func (t *TreeSitterIndexer) getLanguage(lang string) *sitter.Language {
	switch lang {
	case "go":
		return golang.GetLanguage()
	case "python":
		return python.GetLanguage()
	case "typescript":
		return typescript.GetLanguage()
	case "typescriptreact":
		return tsx.GetLanguage()
	case "javascript":
		return typescript.GetLanguage()
	case "java":
		return java.GetLanguage()
	case "swift":
		return swift.GetLanguage()
	case "rust":
		return rust.GetLanguage()
	case "ocaml":
		return ocaml.GetLanguage()
	default:
		return nil
	}
}

// extractSymbols walks the AST and extracts symbol definitions
func (t *TreeSitterIndexer) extractSymbols(node *sitter.Node, content []byte, file FileInfo, scope string) []*db.Symbol {
	var symbols []*db.Symbol

	// Check if this node is a symbol we care about
	if sym := t.nodeToSymbol(node, content, file, scope); sym != nil {
		symbols = append(symbols, sym)
		// Update scope for children
		scope = sym.Name
	}

	// Recursively process children
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		childSymbols := t.extractSymbols(child, content, file, scope)
		symbols = append(symbols, childSymbols...)
	}

	return symbols
}

// nodeToSymbol converts a tree-sitter node to a Symbol if applicable
func (t *TreeSitterIndexer) nodeToSymbol(node *sitter.Node, content []byte, file FileInfo, scope string) *db.Symbol {
	var name, kind, signature string

	switch file.Language {
	case "go":
		name, kind, signature = t.extractGoSymbol(node, content)
	case "python":
		name, kind, signature = t.extractPythonSymbol(node, content)
	case "swift":
		name, kind, signature = t.extractSwiftSymbol(node, content)
	case "typescript", "javascript", "typescriptreact":
		name, kind, signature = t.extractTypeScriptSymbol(node, content)
	case "java":
		name, kind, signature = t.extractJavaSymbol(node, content)
	case "rust":
		name, kind, signature = t.extractRustSymbol(node, content)
	case "ocaml":
		name, kind, signature = t.extractOCamlSymbol(node, content)
	default:
		return nil
	}

	if name == "" {
		return nil
	}

	// Create symbol ID
	id := fmt.Sprintf("%s#%s", file.RelPath, name)
	if scope != "" {
		id = fmt.Sprintf("%s#%s.%s", file.RelPath, scope, name)
	}

	startLine := int(node.StartPoint().Row) + 1
	endLine := int(node.EndPoint().Row) + 1
	startCol := int(node.StartPoint().Column)
	endCol := int(node.EndPoint().Column)

	return &db.Symbol{
		ID:        id,
		Name:      name,
		Kind:      kind,
		File:      file.Path,
		Line:      startLine,
		Column:    startCol,
		EndLine:   &endLine,
		EndColumn: &endCol,
		Scope:     scope,
		Signature: signature,
		Language:  file.Language,
		Source:    "tree-sitter",
		CreatedAt: time.Now(),
	}
}

// Language-specific extractors

func (t *TreeSitterIndexer) extractGoSymbol(node *sitter.Node, content []byte) (name, kind, signature string) {
	switch node.Type() {
	case "function_declaration":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = nameNode.Content(content)
			kind = "function"
			signature = node.Content(content)
			// Truncate signature to first line
			if idx := findNewline(signature); idx > 0 {
				signature = signature[:idx]
			}
		}
	case "method_declaration":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = nameNode.Content(content)
			kind = "method"
			signature = node.Content(content)
			if idx := findNewline(signature); idx > 0 {
				signature = signature[:idx]
			}
		}
	case "type_declaration":
		for i := 0; i < int(node.NamedChildCount()); i++ {
			child := node.NamedChild(i)
			if child.Type() == "type_spec" {
				if nameNode := child.ChildByFieldName("name"); nameNode != nil {
					name = nameNode.Content(content)
					typeNode := child.ChildByFieldName("type")
					if typeNode != nil && typeNode.Type() == "struct_type" {
						kind = "struct"
					} else if typeNode != nil && typeNode.Type() == "interface_type" {
						kind = "interface"
					} else {
						kind = "type"
					}
				}
			}
		}
	case "const_declaration", "var_declaration":
		for i := 0; i < int(node.NamedChildCount()); i++ {
			child := node.NamedChild(i)
			if child.Type() == "const_spec" || child.Type() == "var_spec" {
				if nameNode := child.ChildByFieldName("name"); nameNode != nil {
					name = nameNode.Content(content)
					if node.Type() == "const_declaration" {
						kind = "constant"
					} else {
						kind = "variable"
					}
				}
			}
		}
	}
	return
}

func (t *TreeSitterIndexer) extractPythonSymbol(node *sitter.Node, content []byte) (name, kind, signature string) {
	switch node.Type() {
	case "function_definition":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = nameNode.Content(content)
			kind = "function"
			signature = getFirstLine(node.Content(content))
		}
	case "class_definition":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = nameNode.Content(content)
			kind = "class"
			signature = getFirstLine(node.Content(content))
		}
	}
	return
}

func (t *TreeSitterIndexer) extractSwiftSymbol(node *sitter.Node, content []byte) (name, kind, signature string) {
	switch node.Type() {
	case "function_declaration":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = nameNode.Content(content)
			kind = "function"
			signature = getFirstLine(node.Content(content))
		}
	case "class_declaration":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = nameNode.Content(content)
			kind = "class"
		}
	case "struct_declaration":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = nameNode.Content(content)
			kind = "struct"
		}
	case "protocol_declaration":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = nameNode.Content(content)
			kind = "interface"
		}
	case "enum_declaration":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = nameNode.Content(content)
			kind = "enum"
		}
	}
	return
}

func (t *TreeSitterIndexer) extractTypeScriptSymbol(node *sitter.Node, content []byte) (name, kind, signature string) {
	switch node.Type() {
	case "function_declaration":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = nameNode.Content(content)
			kind = "function"
			signature = getFirstLine(node.Content(content))
		}
	case "class_declaration":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = nameNode.Content(content)
			kind = "class"
		}
	case "interface_declaration":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = nameNode.Content(content)
			kind = "interface"
		}
	case "method_definition":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = nameNode.Content(content)
			kind = "method"
			signature = getFirstLine(node.Content(content))
		}
	}
	return
}

func (t *TreeSitterIndexer) extractJavaSymbol(node *sitter.Node, content []byte) (name, kind, signature string) {
	switch node.Type() {
	case "method_declaration":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = nameNode.Content(content)
			kind = "method"
			signature = getFirstLine(node.Content(content))
		}
	case "class_declaration":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = nameNode.Content(content)
			kind = "class"
		}
	case "interface_declaration":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = nameNode.Content(content)
			kind = "interface"
		}
	case "enum_declaration":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = nameNode.Content(content)
			kind = "enum"
		}
	}
	return
}

func (t *TreeSitterIndexer) extractRustSymbol(node *sitter.Node, content []byte) (name, kind, signature string) {
	switch node.Type() {
	case "function_item":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = nameNode.Content(content)
			kind = "function"
			signature = getFirstLine(node.Content(content))
		}
	case "struct_item":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = nameNode.Content(content)
			kind = "struct"
		}
	case "impl_item":
		// Skip impl blocks, we extract methods from inside
	case "enum_item":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = nameNode.Content(content)
			kind = "enum"
		}
	case "trait_item":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = nameNode.Content(content)
			kind = "interface"
		}
	}
	return
}

func (t *TreeSitterIndexer) extractOCamlSymbol(node *sitter.Node, content []byte) (name, kind, signature string) {
	switch node.Type() {
	case "value_definition":
		// let binding - could be function or value
		if patternNode := node.ChildByFieldName("pattern"); patternNode != nil {
			name = patternNode.Content(content)
			// Check if it has parameters (making it a function)
			if node.ChildByFieldName("body") != nil {
				kind = "function"
			} else {
				kind = "variable"
			}
			signature = getFirstLine(node.Content(content))
		}
	case "let_binding":
		if patternNode := node.ChildByFieldName("pattern"); patternNode != nil {
			name = patternNode.Content(content)
			kind = "function"
			signature = getFirstLine(node.Content(content))
		}
	case "type_definition":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = nameNode.Content(content)
			kind = "type"
		}
	case "module_definition":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = nameNode.Content(content)
			kind = "module"
		}
	case "module_type_definition":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = nameNode.Content(content)
			kind = "interface"
		}
	}
	return
}

// Helper functions

func findNewline(s string) int {
	for i, c := range s {
		if c == '\n' {
			return i
		}
	}
	return -1
}

func getFirstLine(s string) string {
	if idx := findNewline(s); idx > 0 {
		return s[:idx]
	}
	return s
}
