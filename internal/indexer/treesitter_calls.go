package indexer

import (
	"context"
	"fmt"
	"os"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/csharp"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/ocaml"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/swift"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
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

// ExtractCalls extracts call relationships from a file using tree-sitter
func (c *CallExtractor) ExtractCalls(ctx context.Context, file FileInfo) (int, error) {
	lang := c.getLanguage(file.Language)
	if lang == nil {
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

	// Extract all function/method calls
	calls := c.extractCalls(tree.RootNode(), content, file)

	// Insert into database
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

// getLanguage returns the tree-sitter language
func (c *CallExtractor) getLanguage(lang string) *sitter.Language {
	switch lang {
	case "csharp":
		return csharp.GetLanguage()
	case "java":
		return java.GetLanguage()
	case "typescript", "typescriptreact", "javascript":
		return typescript.GetLanguage()
	case "python":
		return python.GetLanguage()
	case "swift":
		return swift.GetLanguage()
	case "rust":
		return rust.GetLanguage()
	case "go":
		return golang.GetLanguage()
	case "ocaml":
		return ocaml.GetLanguage()
	default:
		return nil
	}
}

// extractCalls walks the AST and extracts call relationships
func (c *CallExtractor) extractCalls(node *sitter.Node, content []byte, file FileInfo) []*db.Call {
	var calls []*db.Call

	switch file.Language {
	case "csharp":
		calls = c.extractCSharpCalls(node, content, file)
	case "java":
		calls = c.extractJavaCalls(node, content, file)
	case "typescript", "typescriptreact", "javascript":
		calls = c.extractTypeScriptCalls(node, content, file)
	case "python":
		calls = c.extractPythonCalls(node, content, file)
	case "go":
		calls = c.extractGoCalls(node, content, file)
	case "rust":
		calls = c.extractRustCalls(node, content, file)
	case "swift":
		calls = c.extractSwiftCalls(node, content, file)
	case "ocaml":
		calls = c.extractOCamlCalls(node, content, file)
	}

	return calls
}

// C# call extraction: obj.Method() or Method()
func (c *CallExtractor) extractCSharpCalls(node *sitter.Node, content []byte, file FileInfo) []*db.Call {
	var calls []*db.Call
	var currentFunction string
	var currentFunctionID string

	c.walkTreeWithContext(node, content, file, func(n *sitter.Node, enclosingFunc string, enclosingFuncID string) {
		currentFunction = enclosingFunc
		currentFunctionID = enclosingFuncID

		if n.Type() == "invocation_expression" {
			calleeName := c.getCSharpCalleeName(n, content)
			if calleeName == "" || currentFunctionID == "" {
				return
			}

			// Find the callee symbol in database
			calleeID := c.resolveSymbolID(calleeName, file.Language)
			if calleeID == "" {
				return
			}

			call := &db.Call{
				CallerID: currentFunctionID,
				CalleeID: calleeID,
				File:     file.Path,
				Line:     int(n.StartPoint().Row) + 1,
				Column:   int(n.StartPoint().Column),
			}
			calls = append(calls, call)
		}
	})

	_ = currentFunction // Silence unused warning
	return calls
}

// Java call extraction
func (c *CallExtractor) extractJavaCalls(node *sitter.Node, content []byte, file FileInfo) []*db.Call {
	var calls []*db.Call

	c.walkTreeWithContext(node, content, file, func(n *sitter.Node, enclosingFunc string, enclosingFuncID string) {
		if n.Type() == "method_invocation" {
			calleeName := c.getJavaCalleeName(n, content)
			if calleeName == "" || enclosingFuncID == "" {
				return
			}

			calleeID := c.resolveSymbolID(calleeName, file.Language)
			if calleeID == "" {
				return
			}

			call := &db.Call{
				CallerID: enclosingFuncID,
				CalleeID: calleeID,
				File:     file.Path,
				Line:     int(n.StartPoint().Row) + 1,
				Column:   int(n.StartPoint().Column),
			}
			calls = append(calls, call)
		}
	})

	return calls
}

// TypeScript/JavaScript call extraction
func (c *CallExtractor) extractTypeScriptCalls(node *sitter.Node, content []byte, file FileInfo) []*db.Call {
	var calls []*db.Call

	c.walkTreeWithContext(node, content, file, func(n *sitter.Node, enclosingFunc string, enclosingFuncID string) {
		if n.Type() == "call_expression" {
			calleeName := c.getTypeScriptCalleeName(n, content)
			if calleeName == "" || enclosingFuncID == "" {
				return
			}

			calleeID := c.resolveSymbolID(calleeName, file.Language)
			if calleeID == "" {
				return
			}

			call := &db.Call{
				CallerID: enclosingFuncID,
				CalleeID: calleeID,
				File:     file.Path,
				Line:     int(n.StartPoint().Row) + 1,
				Column:   int(n.StartPoint().Column),
			}
			calls = append(calls, call)
		}
	})

	return calls
}

// Python call extraction
func (c *CallExtractor) extractPythonCalls(node *sitter.Node, content []byte, file FileInfo) []*db.Call {
	var calls []*db.Call

	c.walkTreeWithContext(node, content, file, func(n *sitter.Node, enclosingFunc string, enclosingFuncID string) {
		if n.Type() == "call" {
			calleeName := c.getPythonCalleeName(n, content)
			if calleeName == "" || enclosingFuncID == "" {
				return
			}

			calleeID := c.resolveSymbolID(calleeName, file.Language)
			if calleeID == "" {
				return
			}

			call := &db.Call{
				CallerID: enclosingFuncID,
				CalleeID: calleeID,
				File:     file.Path,
				Line:     int(n.StartPoint().Row) + 1,
				Column:   int(n.StartPoint().Column),
			}
			calls = append(calls, call)
		}
	})

	return calls
}

// Go call extraction
func (c *CallExtractor) extractGoCalls(node *sitter.Node, content []byte, file FileInfo) []*db.Call {
	var calls []*db.Call

	c.walkTreeWithContext(node, content, file, func(n *sitter.Node, enclosingFunc string, enclosingFuncID string) {
		if n.Type() == "call_expression" {
			calleeName := c.getGoCalleeName(n, content)
			if calleeName == "" || enclosingFuncID == "" {
				return
			}

			calleeID := c.resolveSymbolID(calleeName, file.Language)
			if calleeID == "" {
				return
			}

			call := &db.Call{
				CallerID: enclosingFuncID,
				CalleeID: calleeID,
				File:     file.Path,
				Line:     int(n.StartPoint().Row) + 1,
				Column:   int(n.StartPoint().Column),
			}
			calls = append(calls, call)
		}
	})

	return calls
}

// Rust call extraction
func (c *CallExtractor) extractRustCalls(node *sitter.Node, content []byte, file FileInfo) []*db.Call {
	var calls []*db.Call

	c.walkTreeWithContext(node, content, file, func(n *sitter.Node, enclosingFunc string, enclosingFuncID string) {
		if n.Type() == "call_expression" {
			calleeName := c.getRustCalleeName(n, content)
			if calleeName == "" || enclosingFuncID == "" {
				return
			}

			calleeID := c.resolveSymbolID(calleeName, file.Language)
			if calleeID == "" {
				return
			}

			call := &db.Call{
				CallerID: enclosingFuncID,
				CalleeID: calleeID,
				File:     file.Path,
				Line:     int(n.StartPoint().Row) + 1,
				Column:   int(n.StartPoint().Column),
			}
			calls = append(calls, call)
		}
	})

	return calls
}

// Swift call extraction
func (c *CallExtractor) extractSwiftCalls(node *sitter.Node, content []byte, file FileInfo) []*db.Call {
	var calls []*db.Call

	c.walkTreeWithContext(node, content, file, func(n *sitter.Node, enclosingFunc string, enclosingFuncID string) {
		if n.Type() == "call_expression" {
			calleeName := c.getSwiftCalleeName(n, content)
			if calleeName == "" || enclosingFuncID == "" {
				return
			}

			calleeID := c.resolveSymbolID(calleeName, file.Language)
			if calleeID == "" {
				return
			}

			call := &db.Call{
				CallerID: enclosingFuncID,
				CalleeID: calleeID,
				File:     file.Path,
				Line:     int(n.StartPoint().Row) + 1,
				Column:   int(n.StartPoint().Column),
			}
			calls = append(calls, call)
		}
	})

	return calls
}

// walkTreeWithContext walks the tree tracking the enclosing function
func (c *CallExtractor) walkTreeWithContext(node *sitter.Node, content []byte, file FileInfo, callback func(*sitter.Node, string, string)) {
	c.walkWithEnclosing(node, content, file, "", "", callback)
}

func (c *CallExtractor) walkWithEnclosing(node *sitter.Node, content []byte, file FileInfo, enclosingFunc string, enclosingFuncID string, callback func(*sitter.Node, string, string)) {
	// Check if this node is a function/method definition
	newFunc, newFuncID := c.getFunctionName(node, content, file)
	if newFunc != "" {
		enclosingFunc = newFunc
		enclosingFuncID = newFuncID
	}

	callback(node, enclosingFunc, enclosingFuncID)

	for i := 0; i < int(node.NamedChildCount()); i++ {
		c.walkWithEnclosing(node.NamedChild(i), content, file, enclosingFunc, enclosingFuncID, callback)
	}
}

// getFunctionName extracts function name if this node is a function definition
func (c *CallExtractor) getFunctionName(node *sitter.Node, content []byte, file FileInfo) (string, string) {
	switch file.Language {
	case "csharp":
		if node.Type() == "method_declaration" || node.Type() == "constructor_declaration" {
			nameNode := node.ChildByFieldName("name")
			if nameNode != nil {
				name := nameNode.Content(content)
				// Find enclosing class
				className := c.getEnclosingClassName(node, content, file.Language)
				fullName := name
				if className != "" {
					fullName = className + "." + name
				}
				return fullName, fmt.Sprintf("%s#%s", file.RelPath, fullName)
			}
		}
	case "java":
		if node.Type() == "method_declaration" || node.Type() == "constructor_declaration" {
			nameNode := node.ChildByFieldName("name")
			if nameNode != nil {
				name := nameNode.Content(content)
				className := c.getEnclosingClassName(node, content, file.Language)
				fullName := name
				if className != "" {
					fullName = className + "." + name
				}
				return fullName, fmt.Sprintf("%s#%s", file.RelPath, fullName)
			}
		}
	case "typescript", "typescriptreact", "javascript":
		if node.Type() == "function_declaration" || node.Type() == "method_definition" {
			nameNode := node.ChildByFieldName("name")
			if nameNode != nil {
				name := nameNode.Content(content)
				return name, fmt.Sprintf("%s#%s", file.RelPath, name)
			}
		}
	case "python":
		if node.Type() == "function_definition" {
			nameNode := node.ChildByFieldName("name")
			if nameNode != nil {
				name := nameNode.Content(content)
				return name, fmt.Sprintf("%s#%s", file.RelPath, name)
			}
		}
	case "go":
		if node.Type() == "function_declaration" || node.Type() == "method_declaration" {
			nameNode := node.ChildByFieldName("name")
			if nameNode != nil {
				name := nameNode.Content(content)
				return name, fmt.Sprintf("%s#%s", file.RelPath, name)
			}
		}
	case "rust":
		if node.Type() == "function_item" {
			nameNode := node.ChildByFieldName("name")
			if nameNode != nil {
				name := nameNode.Content(content)
				return name, fmt.Sprintf("%s#%s", file.RelPath, name)
			}
		}
	case "swift":
		if node.Type() == "function_declaration" {
			nameNode := node.ChildByFieldName("name")
			if nameNode != nil {
				name := nameNode.Content(content)
				return name, fmt.Sprintf("%s#%s", file.RelPath, name)
			}
		}
	case "ocaml":
		// OCaml function definitions: let_binding or value_definition
		if node.Type() == "let_binding" || node.Type() == "value_definition" {
			patternNode := node.ChildByFieldName("pattern")
			if patternNode != nil {
				name := patternNode.Content(content)
				return name, fmt.Sprintf("%s#%s", file.RelPath, name)
			}
		}
	}
	return "", ""
}

// getEnclosingClassName finds the name of the enclosing class
func (c *CallExtractor) getEnclosingClassName(node *sitter.Node, content []byte, language string) string {
	parent := node.Parent()
	for parent != nil {
		switch language {
		case "csharp":
			if parent.Type() == "class_declaration" || parent.Type() == "struct_declaration" {
				nameNode := parent.ChildByFieldName("name")
				if nameNode != nil {
					return nameNode.Content(content)
				}
				// Fallback: find identifier child
				for i := 0; i < int(parent.NamedChildCount()); i++ {
					child := parent.NamedChild(i)
					if child.Type() == "identifier" {
						return child.Content(content)
					}
				}
			}
		case "java":
			if parent.Type() == "class_declaration" {
				for i := 0; i < int(parent.NamedChildCount()); i++ {
					child := parent.NamedChild(i)
					if child.Type() == "identifier" {
						return child.Content(content)
					}
				}
			}
		}
		parent = parent.Parent()
	}
	return ""
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

// Language-specific callee name extractors

func (c *CallExtractor) getCSharpCalleeName(node *sitter.Node, content []byte) string {
	// invocation_expression: (member_access_expression) (argument_list)
	// or: (identifier) (argument_list)
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child.Type() == "member_access_expression" {
			// Get the method name (last identifier in the chain)
			nameNode := child.ChildByFieldName("name")
			if nameNode != nil {
				return nameNode.Content(content)
			}
		} else if child.Type() == "identifier" {
			return child.Content(content)
		}
	}
	return ""
}

func (c *CallExtractor) getJavaCalleeName(node *sitter.Node, content []byte) string {
	// method_invocation: (identifier) or (field_access).(identifier)(arguments)
	nameNode := node.ChildByFieldName("name")
	if nameNode != nil {
		return nameNode.Content(content)
	}
	return ""
}

func (c *CallExtractor) getTypeScriptCalleeName(node *sitter.Node, content []byte) string {
	// call_expression -> function field
	funcNode := node.ChildByFieldName("function")
	if funcNode == nil && node.NamedChildCount() > 0 {
		funcNode = node.NamedChild(0)
	}
	if funcNode != nil {
		if funcNode.Type() == "member_expression" {
			// obj.method() -> get property
			propNode := funcNode.ChildByFieldName("property")
			if propNode != nil {
				return propNode.Content(content)
			}
		} else if funcNode.Type() == "identifier" {
			return funcNode.Content(content)
		}
	}
	return ""
}

func (c *CallExtractor) getPythonCalleeName(node *sitter.Node, content []byte) string {
	// call -> function field
	funcNode := node.ChildByFieldName("function")
	if funcNode != nil {
		if funcNode.Type() == "attribute" {
			// Get the attribute name
			attrNode := funcNode.ChildByFieldName("attribute")
			if attrNode != nil {
				return attrNode.Content(content)
			}
		} else if funcNode.Type() == "identifier" {
			return funcNode.Content(content)
		}
	}
	return ""
}

func (c *CallExtractor) getGoCalleeName(node *sitter.Node, content []byte) string {
	// call_expression -> function field
	funcNode := node.ChildByFieldName("function")
	if funcNode != nil {
		if funcNode.Type() == "selector_expression" {
			// obj.Method() -> get field
			fieldNode := funcNode.ChildByFieldName("field")
			if fieldNode != nil {
				return fieldNode.Content(content)
			}
		} else if funcNode.Type() == "identifier" {
			return funcNode.Content(content)
		}
	}
	return ""
}

func (c *CallExtractor) getRustCalleeName(node *sitter.Node, content []byte) string {
	// call_expression -> function field
	funcNode := node.ChildByFieldName("function")
	if funcNode != nil {
		if funcNode.Type() == "field_expression" {
			fieldNode := funcNode.ChildByFieldName("field")
			if fieldNode != nil {
				return fieldNode.Content(content)
			}
		} else if funcNode.Type() == "identifier" {
			return funcNode.Content(content)
		} else if funcNode.Type() == "scoped_identifier" {
			// Get the last part
			if funcNode.NamedChildCount() > 0 {
				return funcNode.NamedChild(int(funcNode.NamedChildCount()) - 1).Content(content)
			}
		}
	}
	return ""
}

func (c *CallExtractor) getSwiftCalleeName(node *sitter.Node, content []byte) string {
	// Try to get function name from first child
	if node.NamedChildCount() > 0 {
		funcNode := node.NamedChild(0)
		if funcNode.Type() == "navigation_expression" {
			// Get the last identifier
			suffixNode := funcNode.ChildByFieldName("suffix")
			if suffixNode != nil {
				return suffixNode.Content(content)
			}
		} else if funcNode.Type() == "simple_identifier" {
			return funcNode.Content(content)
		}
	}
	return ""
}

// OCaml call extraction
func (c *CallExtractor) extractOCamlCalls(node *sitter.Node, content []byte, file FileInfo) []*db.Call {
	var calls []*db.Call

	c.walkTreeWithContext(node, content, file, func(n *sitter.Node, enclosingFunc string, enclosingFuncID string) {
		// OCaml function application: (application_expression)
		if n.Type() == "application_expression" {
			calleeName := c.getOCamlCalleeName(n, content)
			if calleeName == "" || enclosingFuncID == "" {
				return
			}

			calleeID := c.resolveSymbolID(calleeName, file.Language)
			if calleeID == "" {
				return
			}

			call := &db.Call{
				CallerID: enclosingFuncID,
				CalleeID: calleeID,
				File:     file.Path,
				Line:     int(n.StartPoint().Row) + 1,
				Column:   int(n.StartPoint().Column),
			}
			calls = append(calls, call)
		}
	})

	return calls
}

func (c *CallExtractor) getOCamlCalleeName(node *sitter.Node, content []byte) string {
	// application_expression has a "function" field
	funcNode := node.ChildByFieldName("function")
	if funcNode == nil && node.NamedChildCount() > 0 {
		// Fallback to first child
		funcNode = node.NamedChild(0)
	}
	if funcNode == nil {
		return ""
	}

	// Extract the function name based on node type
	switch funcNode.Type() {
	case "value_path":
		// Module.func - get the last part (the actual function name)
		if funcNode.NamedChildCount() > 0 {
			lastPart := funcNode.NamedChild(int(funcNode.NamedChildCount()) - 1)
			return lastPart.Content(content)
		}
		return funcNode.Content(content)
	case "value_name":
		return funcNode.Content(content)
	case "field_get_expression":
		// Module.function or record.field
		fieldNode := funcNode.ChildByFieldName("field")
		if fieldNode != nil {
			return fieldNode.Content(content)
		}
	case "constructor_path":
		// For variant constructors like Error, Success
		if funcNode.NamedChildCount() > 0 {
			lastPart := funcNode.NamedChild(int(funcNode.NamedChildCount()) - 1)
			return lastPart.Content(content)
		}
		return funcNode.Content(content)
	}

	// Try getting the content directly if it's a simple identifier
	return funcNode.Content(content)
}

// Helper to check if a string contains another (case insensitive)
func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
