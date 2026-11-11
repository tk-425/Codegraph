package indexer

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/tk-425/Codegraph/internal/db"
)

// TreeSitterIndexer provides fallback symbol extraction using tree-sitter
type TreeSitterIndexer struct {
	store    graphStore
	rootPath string
}

// NewTreeSitterIndexer creates a new tree-sitter based indexer
func NewTreeSitterIndexer(store graphStore, rootPath string) *TreeSitterIndexer {
	return &TreeSitterIndexer{
		store:    store,
		rootPath: rootPath,
	}
}

// IndexFile extracts symbols from a file using tree-sitter
func (t *TreeSitterIndexer) IndexFile(ctx context.Context, file FileInfo) (int, error) {
	// A provider attempt starts fresh for this file, so a fallback replaces
	// what a failed attempt left instead of appending to it.
	t.store.BeginFile(file.Path)

	// Get the appropriate language
	lang := grammar(file.Language)
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
	symbols := t.extractSymbols(tree.RootNode(), content, file, nil, idAllocator{})

	// Store symbols in database
	for _, sym := range symbols {
		if err := t.store.InsertSymbol(sym); err != nil {
			return 0, err
		}
	}

	// Update file metadata
	if err := t.store.UpdateFileMeta(file.Path, time.Now(), file.Language); err != nil {
		return 0, err
	}

	return len(symbols), nil
}

// extractSymbols walks the AST and extracts symbol definitions
// owners is the lexical owner chain of node, outermost first.
func (t *TreeSitterIndexer) extractSymbols(node *sitter.Node, content []byte, file FileInfo, owners []string, ids idAllocator) []*db.Symbol {
	var symbols []*db.Symbol

	// Check if this node is a symbol we care about
	if sym, key := t.nodeToSymbol(node, content, file, owners); sym != nil {
		sym.ID = ids.assign(key)
		symbols = append(symbols, sym)
		// Children are owned by this symbol
		owners = append(append([]string(nil), key.Owners...), key.Name)
	}

	// Recursively process children
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		childSymbols := t.extractSymbols(child, content, file, owners, ids)
		symbols = append(symbols, childSymbols...)
	}

	return symbols
}

// nodeToSymbol converts a tree-sitter node to a Symbol if applicable
func (t *TreeSitterIndexer) nodeToSymbol(node *sitter.Node, content []byte, file FileInfo, owners []string) (*db.Symbol, symbolKey) {
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
	case "c":
		name, kind, signature = t.extractCSymbol(node, content)
	case "cpp":
		name, kind, signature = t.extractCppSymbol(node, content)
	case "csharp":
		name, kind, signature = t.extractCSharpSymbol(node, content)
	default:
		return nil, symbolKey{}
	}

	if name == "" {
		return nil, symbolKey{}
	}

	key := symbolKey{Language: file.Language, RelPath: file.RelPath, Owners: owners, Name: name}
	if file.Language == "go" && node.Type() == "method_declaration" {
		if receiver := goMethodReceiver(node, content); receiver != "" {
			key.Owners = append(append([]string(nil), owners...), receiver)
		}
	}

	// The stored position is the name's; the declaration range is kept for
	// containment.
	startLine, startCol := nodePosition(node)
	line, column := startLine, startCol
	if nameNode := declarationName(node, name, content); nameNode != nil {
		line, column = nodePosition(nameNode)
	}
	endLine := int(node.EndPoint().Row) + 1
	endCol := int(node.EndPoint().Column)

	return &db.Symbol{
		ID:          key.ID(),
		Name:        name,
		Kind:        kind,
		File:        file.Path,
		Line:        line,
		Column:      column,
		StartLine:   startLine,
		StartColumn: startCol,
		EndLine:     &endLine,
		EndColumn:   &endCol,
		Scope:       key.Scope(),
		Signature:   signature,
		Language:    file.Language,
		Source:      "tree-sitter",
		CreatedAt:   time.Now(),
	}, key
}

// declarationName finds the node spelling name in a declaration: its "name"
// field, else the first leaf with that text.
func declarationName(node *sitter.Node, name string, content []byte) *sitter.Node {
	if nameNode := node.ChildByFieldName("name"); nameNode != nil && nameNode.Content(content) == name {
		return nameNode
	}
	var found *sitter.Node
	walk(node, func(n *sitter.Node) {
		if found == nil && n.NamedChildCount() == 0 && n.Content(content) == name {
			found = n
		}
	})
	return found
}

// goMethodReceiver returns the receiver type name of a Go method declaration.
func goMethodReceiver(node *sitter.Node, content []byte) string {
	receiver := node.ChildByFieldName("receiver")
	if receiver == nil {
		return ""
	}
	for i := 0; i < int(receiver.NamedChildCount()); i++ {
		param := receiver.NamedChild(i)
		if param.Type() != "parameter_declaration" {
			continue
		}
		if typeNode := param.ChildByFieldName("type"); typeNode != nil {
			return goReceiverType(typeNode.Content(content))
		}
	}
	return ""
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
			// Extract type signature from val declarations
			signature = extractOCamlSignature(node, content)
		}
	case "let_binding":
		if patternNode := node.ChildByFieldName("pattern"); patternNode != nil {
			name = patternNode.Content(content)
			kind = "function"
			signature = extractOCamlSignature(node, content)
		}
	case "value_specification":
		// val declarations in .mli files: val add : float -> float -> float
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = nameNode.Content(content)
			kind = "function"
			// Get the type signature
			if typeNode := node.ChildByFieldName("type"); typeNode != nil {
				fullType := typeNode.Content(content)
				// Extract return type (last part after ->)
				if idx := strings.LastIndex(fullType, "->"); idx != -1 {
					signature = strings.TrimSpace(fullType[idx+2:])
				} else {
					signature = fullType
				}
			}
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

// extractOCamlSignature extracts return type from OCaml function definitions
func extractOCamlSignature(node *sitter.Node, content []byte) string {
	// Look for type annotation in the node
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child.Type() == "type_annotation" || child.Type() == "typed_pattern" {
			typeContent := child.Content(content)
			// Extract return type (last part after ->)
			if idx := strings.LastIndex(typeContent, "->"); idx != -1 {
				return strings.TrimSpace(typeContent[idx+2:])
			}
			return typeContent
		}
	}
	return ""
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

func (t *TreeSitterIndexer) extractCSymbol(node *sitter.Node, content []byte) (name, kind, signature string) {
	switch node.Type() {
	case "function_definition":
		if declarator := node.ChildByFieldName("declarator"); declarator != nil {
			if funcDecl := declarator.ChildByFieldName("declarator"); funcDecl != nil {
				name = funcDecl.Content(content)
			} else {
				name = declarator.Content(content)
			}
			if idx := findParen(name); idx > 0 {
				name = name[:idx]
			}
			kind = "function"
			signature = getFirstLine(node.Content(content))
		}
	case "struct_specifier":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = nameNode.Content(content)
			kind = "struct"
		}
	case "enum_specifier":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = nameNode.Content(content)
			kind = "enum"
		}
	}
	return
}

func (t *TreeSitterIndexer) extractCppSymbol(node *sitter.Node, content []byte) (name, kind, signature string) {
	switch node.Type() {
	case "function_definition":
		if declarator := node.ChildByFieldName("declarator"); declarator != nil {
			name = declarator.Content(content)
			if idx := findParen(name); idx > 0 {
				name = name[:idx]
			}
			kind = "function"
			signature = getFirstLine(node.Content(content))
		}
	case "class_specifier":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = nameNode.Content(content)
			kind = "class"
		}
	case "struct_specifier":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = nameNode.Content(content)
			kind = "struct"
		}
	}
	return
}

func (t *TreeSitterIndexer) extractCSharpSymbol(node *sitter.Node, content []byte) (name, kind, signature string) {
	switch node.Type() {
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
	case "method_declaration":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = nameNode.Content(content)
			kind = "method"
			// Extract just the return type, not the full declaration
			if returnType := node.ChildByFieldName("type"); returnType != nil {
				signature = returnType.Content(content)
			} else {
				// Fallback: look for predefined_type or type child
				for i := 0; i < int(node.NamedChildCount()); i++ {
					child := node.NamedChild(i)
					if child.Type() == "predefined_type" || child.Type() == "type" || child.Type() == "void_keyword" {
						signature = child.Content(content)
						break
					}
				}
			}
		}
	case "struct_declaration":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = nameNode.Content(content)
			kind = "struct"
		}
	case "enum_declaration":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name = nameNode.Content(content)
			kind = "enum"
		}
	}
	return
}

func findParen(s string) int {
	for i, c := range s {
		if c == '(' {
			return i
		}
	}
	return -1
}
