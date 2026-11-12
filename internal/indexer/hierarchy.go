package indexer

import (
	"context"
	"fmt"
	"os"
	"time"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/tk-425/Codegraph/internal/db"
	"github.com/tk-425/Codegraph/internal/lsp"
)

// HierarchyIndexer extracts type hierarchy relationships
type HierarchyIndexer struct {
	store    graphStore
	lsp      *lsp.Manager
	rootPath string
	// failures counts failed requests of the latest IndexHierarchyLSP call.
	failures requestFailures
}

// NewHierarchyIndexer creates a new hierarchy indexer
func NewHierarchyIndexer(store graphStore, lspManager *lsp.Manager, rootPath string) *HierarchyIndexer {
	return &HierarchyIndexer{
		store:    store,
		lsp:      lspManager,
		rootPath: rootPath,
	}
}

// IndexHierarchyLSP extracts type hierarchy using LSP typeHierarchy requests
func (h *HierarchyIndexer) IndexHierarchyLSP(ctx context.Context, language string) (int, error) {
	h.failures = requestFailures{}
	client, err := h.lsp.GetClient(ctx, language)
	if err != nil {
		return 0, fmt.Errorf("failed to get LSP client: %w", err)
	}

	// The published hierarchy of this language is replaced at publication
	// (db.Manager.Publish), not here: extraction output is a candidate
	// until then (R05).

	// Get all class/interface symbols
	symbols, err := h.store.GetTypeSymbols(language)
	if err != nil {
		return 0, fmt.Errorf("failed to get type symbols: %w", err)
	}

	count := 0
	openedFiles := make(map[string]bool)
	lines := lsp.NewSourceLines()
	enc := client.PositionEncoding()
	pass := &hierarchyPass{
		store:    h.store,
		language: language,
		root:     lsp.NewRoot(h.rootPath),
		lines:    lines,
		enc:      enc,
		symbols:  map[string]fileSymbols{},
	}

	for _, sym := range symbols {
		fileURI := lsp.PathToURI(sym.File)

		// Open file if not already opened
		if !openedFiles[fileURI] {
			content, err := os.ReadFile(sym.File)
			if err != nil {
				return count, err
			}
			if err := client.DidOpenTextDocument(fileURI, language, string(content)); err != nil {
				h.failures.add(client, err)
				continue
			}
			openedFiles[fileURI] = true
		}

		// Get supertypes for this symbol
		pos := lines.ToLSP(enc, sym.File, sym.Line, sym.Column)

		// Prepare type hierarchy at this position
		items, err := client.PrepareTypeHierarchy(ctx, fileURI, pos)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return count, ctxErr
			}
			// Not all LSPs support type hierarchy; add ignores unsupported.
			h.failures.add(client, err)
			continue
		}
		if len(items) == 0 {
			continue
		}

		// Get supertypes for the first item
		supertypes, err := client.Supertypes(ctx, items[0])
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return count, ctxErr
			}
			h.failures.add(client, err)
			continue
		}

		for _, parent := range supertypes {
			stored, err := pass.parent(sym, parent)
			if err != nil {
				return count, err
			}
			if stored {
				count++
			}
		}
	}

	// Close opened files
	for fileURI := range openedFiles {
		client.DidCloseTextDocument(fileURI)
	}

	return count, nil
}

// hierarchyPass holds what one hierarchy pass learns about each file.
type hierarchyPass struct {
	store    graphStore
	language string
	root     lsp.Root
	lines    *lsp.SourceLines
	enc      lsp.PositionEncoding
	symbols  map[string]fileSymbols
}

func (p *hierarchyPass) fileSymbols(path string) (fileSymbols, error) {
	if symbols, ok := p.symbols[path]; ok {
		return symbols, nil
	}
	symbols, err := loadFileSymbols(p.store, path)
	if err != nil {
		return nil, err
	}
	p.symbols[path] = symbols
	return symbols, nil
}

// parent stores the supertype item of child as an edge to the indexed type
// declared at the item's position. A parent outside the indexed project is
// recorded as external, and one nothing indexed declares there as
// unresolved; neither becomes an edge. It reports whether an edge was stored.
func (p *hierarchyPass) parent(child db.Symbol, item lsp.TypeHierarchyItem) (bool, error) {
	var parent *db.Symbol
	reason := reasonExternal
	if path, err := p.root.Path(item.URI); err == nil {
		symbols, err := p.fileSymbols(path)
		if err != nil {
			return false, err
		}
		if len(symbols) > 0 {
			reason = reasonUnresolved
			line, column := p.lines.FromLSP(p.enc, path, item.SelectionRange.Start)
			parent = symbols.named(line, column)
			if parent == nil || !typeKinds[parent.Kind] {
				parent = symbols.enclosing(line, column, typeKinds)
			}
		}
	}

	// Implementing is a non-interface taking an interface as its parent;
	// everything else, interfaces extending interfaces included, extends.
	parentIsInterface := item.Kind == lsp.SymbolKindInterface
	if parent != nil {
		parentIsInterface = parent.Kind == "interface"
	}
	relationship := "extends"
	if parentIsInterface && child.Kind != "interface" {
		relationship = "implements"
	}

	if parent == nil {
		return false, p.store.InsertUnresolved(&db.Unresolved{
			Kind:       relationship,
			Language:   p.language,
			FromID:     child.ID,
			TargetName: item.Name,
			File:       child.File,
			Line:       child.Line,
			Column:     child.Column,
			Reason:     reason,
		})
	}
	err := p.store.InsertTypeHierarchy(&db.TypeHierarchy{
		Evidence:     "lsp-type-hierarchy",
		ChildID:      child.ID,
		ParentID:     parent.ID,
		Relationship: relationship,
	})
	return err == nil, err
}

// IndexHierarchyTreeSitter extracts type hierarchy using tree-sitter parsing
func (h *HierarchyIndexer) IndexHierarchyTreeSitter(ctx context.Context, file FileInfo) (int, error) {
	lang := grammar(file.Language)
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

	// Extractors name both ends; each resolves on evidence, the child usually
	// to the type declared in this file. Rust names a type declared elsewhere.
	relationships := h.extractHierarchy(tree.RootNode(), content, file)
	at := evidence{language: file.Language, file: file.Path}

	count := 0
	for _, rel := range relationships {
		site := &db.Unresolved{Kind: rel.Relationship, Language: file.Language, File: file.Path}
		child, err := resolveTarget(h.store, rel.ChildID, typeKinds, at)
		if err != nil {
			return count, fmt.Errorf("failed to resolve %s: %w", rel.ChildID, err)
		}
		if child.symbol == nil {
			site.TargetName, site.Reason, site.Candidates = rel.ChildID, child.reason, child.candidates
			if err := h.store.InsertUnresolved(site); err != nil {
				return count, err
			}
			continue
		}
		site.FromID, site.Line, site.Column = child.symbol.ID, child.symbol.Line, child.symbol.Column

		parent, err := resolveTarget(h.store, rel.ParentID, typeKinds, at)
		if err != nil {
			return count, fmt.Errorf("failed to resolve %s: %w", rel.ParentID, err)
		}
		if parent.symbol == nil {
			site.TargetName, site.Reason, site.Candidates = rel.ParentID, parent.reason, parent.candidates
			if err := h.store.InsertUnresolved(site); err != nil {
				return count, err
			}
			continue
		}
		rel.ChildID, rel.ParentID = child.symbol.ID, parent.symbol.ID
		rel.Evidence = "tree-sitter-hierarchy"
		if err := h.store.InsertTypeHierarchy(rel); err != nil {
			return count, err
		}
		count++
	}

	return count, nil
}

// extractHierarchy walks the AST and extracts type relationships
func (h *HierarchyIndexer) extractHierarchy(node *sitter.Node, content []byte, file FileInfo) []*db.TypeHierarchy {
	var relationships []*db.TypeHierarchy

	switch file.Language {
	case "csharp":
		relationships = h.extractCSharpHierarchy(node, content, file)
	case "java":
		relationships = h.extractJavaHierarchy(node, content, file)
	case "typescript", "typescriptreact", "javascript":
		relationships = h.extractTypeScriptHierarchy(node, content, file)
	case "python":
		relationships = h.extractPythonHierarchy(node, content, file)
	case "swift":
		relationships = h.extractSwiftHierarchy(node, content, file)
	case "rust":
		relationships = h.extractRustHierarchy(node, content, file)
	case "go":
		relationships = h.extractGoHierarchy(node, content, file)
	case "ocaml":
		relationships = h.extractOCamlHierarchy(node, content, file)
	}

	return relationships
}

// C# hierarchy: class Foo : IBar, BaseClass
func (h *HierarchyIndexer) extractCSharpHierarchy(node *sitter.Node, content []byte, file FileInfo) []*db.TypeHierarchy {
	var relationships []*db.TypeHierarchy

	walk(node, func(n *sitter.Node) {
		if n.Type() != "class_declaration" && n.Type() != "struct_declaration" {
			return
		}

		// Find class name and base_list by iterating children
		var className string
		var baseList *sitter.Node

		for i := 0; i < int(n.NamedChildCount()); i++ {
			child := n.NamedChild(i)
			switch child.Type() {
			case "identifier":
				className = child.Content(content)
			case "base_list":
				baseList = child
			}
		}

		if className == "" {
			return
		}

		childID := className

		// Extract parent types from base_list
		if baseList != nil {
			for j := 0; j < int(baseList.NamedChildCount()); j++ {
				baseType := baseList.NamedChild(j)
				parentName := h.getTypeName(baseType, content)
				if parentName == "" {
					continue
				}

				relationship := "implements"
				// First base type could be a class (extends), rest are interfaces
				if j == 0 && !startsWithI(parentName) {
					relationship = "extends"
				}

				relationships = append(relationships, &db.TypeHierarchy{
					ChildID:      childID,
					ParentID:     parentName,
					Relationship: relationship,
				})
			}
		}
	})

	return relationships
}

// Java hierarchy: class Foo extends Bar implements IBaz
func (h *HierarchyIndexer) extractJavaHierarchy(node *sitter.Node, content []byte, file FileInfo) []*db.TypeHierarchy {
	var relationships []*db.TypeHierarchy

	walk(node, func(n *sitter.Node) {
		if n.Type() != "class_declaration" {
			return
		}

		// Find class name, superclass, and interfaces by iterating children
		var className string
		var superclass *sitter.Node
		var interfaces *sitter.Node

		for i := 0; i < int(n.NamedChildCount()); i++ {
			child := n.NamedChild(i)
			switch child.Type() {
			case "identifier":
				className = child.Content(content)
			case "superclass":
				superclass = child
			case "super_interfaces":
				interfaces = child
			}
		}

		if className == "" {
			return
		}

		childID := className

		// Extract superclass
		if superclass != nil {
			// superclass contains type_identifier
			for i := 0; i < int(superclass.NamedChildCount()); i++ {
				typeNode := superclass.NamedChild(i)
				parentName := h.getTypeName(typeNode, content)
				if parentName != "" {
					relationships = append(relationships, &db.TypeHierarchy{
						ChildID:      childID,
						ParentID:     parentName,
						Relationship: "extends",
					})
				}
			}
		}

		// Extract interfaces (super_interfaces -> type_list -> type_identifier)
		if interfaces != nil {
			// Look for type_list inside super_interfaces
			for i := 0; i < int(interfaces.NamedChildCount()); i++ {
				typeList := interfaces.NamedChild(i)
				if typeList.Type() == "type_list" {
					for j := 0; j < int(typeList.NamedChildCount()); j++ {
						typeNode := typeList.NamedChild(j)
						parentName := h.getTypeName(typeNode, content)
						if parentName != "" {
							relationships = append(relationships, &db.TypeHierarchy{
								ChildID:      childID,
								ParentID:     parentName,
								Relationship: "implements",
							})
						}
					}
				}
			}
		}
	})

	return relationships
}

// TypeScript hierarchy: class Foo extends Bar implements IBaz
func (h *HierarchyIndexer) extractTypeScriptHierarchy(node *sitter.Node, content []byte, file FileInfo) []*db.TypeHierarchy {
	var relationships []*db.TypeHierarchy

	walk(node, func(n *sitter.Node) {
		if n.Type() != "class_declaration" {
			return
		}

		nameNode := n.ChildByFieldName("name")
		if nameNode == nil {
			return
		}
		className := nameNode.Content(content)
		childID := className

		// Look for heritage clause (extends/implements)
		for i := 0; i < int(n.NamedChildCount()); i++ {
			child := n.NamedChild(i)
			if child.Type() == "class_heritage" {
				for j := 0; j < int(child.NamedChildCount()); j++ {
					clause := child.NamedChild(j)
					if clause.Type() == "extends_clause" {
						// extends
						for k := 0; k < int(clause.NamedChildCount()); k++ {
							typeNode := clause.NamedChild(k)
							parentName := h.getTypeName(typeNode, content)
							relationships = append(relationships, &db.TypeHierarchy{
								ChildID:      childID,
								ParentID:     parentName,
								Relationship: "extends",
							})
						}
					} else if clause.Type() == "implements_clause" {
						// implements
						for k := 0; k < int(clause.NamedChildCount()); k++ {
							typeNode := clause.NamedChild(k)
							parentName := h.getTypeName(typeNode, content)
							relationships = append(relationships, &db.TypeHierarchy{
								ChildID:      childID,
								ParentID:     parentName,
								Relationship: "implements",
							})
						}
					}
				}
			}
		}
	})

	return relationships
}

// Python hierarchy: class Foo(Base, Mixin):
func (h *HierarchyIndexer) extractPythonHierarchy(node *sitter.Node, content []byte, file FileInfo) []*db.TypeHierarchy {
	var relationships []*db.TypeHierarchy

	walk(node, func(n *sitter.Node) {
		if n.Type() != "class_definition" {
			return
		}

		nameNode := n.ChildByFieldName("name")
		if nameNode == nil {
			return
		}
		className := nameNode.Content(content)
		childID := className

		// Look for argument_list (base classes)
		superclassNode := n.ChildByFieldName("superclasses")
		if superclassNode != nil {
			for i := 0; i < int(superclassNode.NamedChildCount()); i++ {
				base := superclassNode.NamedChild(i)
				parentName := h.getTypeName(base, content)
				// Python doesn't distinguish extends vs implements
				relationships = append(relationships, &db.TypeHierarchy{
					ChildID:      childID,
					ParentID:     parentName,
					Relationship: "extends",
				})
			}
		}
	})

	return relationships
}

// Swift hierarchy: class Foo: Base, Protocol
func (h *HierarchyIndexer) extractSwiftHierarchy(node *sitter.Node, content []byte, file FileInfo) []*db.TypeHierarchy {
	var relationships []*db.TypeHierarchy

	walk(node, func(n *sitter.Node) {
		if n.Type() != "class_declaration" && n.Type() != "struct_declaration" {
			return
		}

		nameNode := n.ChildByFieldName("name")
		if nameNode == nil {
			return
		}
		className := nameNode.Content(content)
		childID := className

		// Look for inheritance clause
		for i := 0; i < int(n.NamedChildCount()); i++ {
			child := n.NamedChild(i)
			if child.Type() == "type_inheritance_clause" {
				for j := 0; j < int(child.NamedChildCount()); j++ {
					typeNode := child.NamedChild(j)
					parentName := h.getTypeName(typeNode, content)
					// First is typically superclass, rest are protocols
					relationship := "extends"
					if j > 0 {
						relationship = "implements"
					}
					relationships = append(relationships, &db.TypeHierarchy{
						ChildID:      childID,
						ParentID:     parentName,
						Relationship: relationship,
					})
				}
			}
		}
	})

	return relationships
}

// Rust hierarchy: impl Trait for Struct
func (h *HierarchyIndexer) extractRustHierarchy(node *sitter.Node, content []byte, file FileInfo) []*db.TypeHierarchy {
	var relationships []*db.TypeHierarchy

	walk(node, func(n *sitter.Node) {
		if n.Type() != "impl_item" {
			return
		}

		// Check if this is "impl Trait for Type"
		traitNode := n.ChildByFieldName("trait")
		typeNode := n.ChildByFieldName("type")

		if traitNode != nil && typeNode != nil {
			traitName := h.getTypeName(traitNode, content)
			typeName := h.getTypeName(typeNode, content)
			childID := typeName

			relationships = append(relationships, &db.TypeHierarchy{
				ChildID:      childID,
				ParentID:     traitName,
				Relationship: "implements",
			})
		}
	})

	return relationships
}

// Go hierarchy: implicit interfaces - detect by embedding
func (h *HierarchyIndexer) extractGoHierarchy(node *sitter.Node, content []byte, file FileInfo) []*db.TypeHierarchy {
	var relationships []*db.TypeHierarchy

	walk(node, func(n *sitter.Node) {
		if n.Type() != "type_declaration" {
			return
		}

		for i := 0; i < int(n.NamedChildCount()); i++ {
			child := n.NamedChild(i)
			if child.Type() != "type_spec" {
				continue
			}

			nameNode := child.ChildByFieldName("name")
			typeNode := child.ChildByFieldName("type")
			if nameNode == nil || typeNode == nil {
				continue
			}

			typeName := nameNode.Content(content)
			childID := typeName

			// Check for struct with embedded types
			if typeNode.Type() == "struct_type" {
				for j := 0; j < int(typeNode.NamedChildCount()); j++ {
					field := typeNode.NamedChild(j)
					if field.Type() == "field_declaration" {
						// Embedded field has no name, just a type
						if field.ChildByFieldName("name") == nil {
							typeField := field.ChildByFieldName("type")
							if typeField != nil {
								parentName := h.getTypeName(typeField, content)
								relationships = append(relationships, &db.TypeHierarchy{
									ChildID:      childID,
									ParentID:     parentName,
									Relationship: "embeds",
								})
							}
						}
					}
				}
			}
		}
	})

	return relationships
}

// OCaml hierarchy: module Calculator : ICalculator = struct ... end
func (h *HierarchyIndexer) extractOCamlHierarchy(node *sitter.Node, content []byte, file FileInfo) []*db.TypeHierarchy {
	var relationships []*db.TypeHierarchy

	walk(node, func(n *sitter.Node) {
		if n.Type() != "module_definition" {
			return
		}

		// Find module_binding child
		for i := 0; i < int(n.NamedChildCount()); i++ {
			child := n.NamedChild(i)
			if child.Type() != "module_binding" {
				continue
			}

			// Find module_name and module_type in module_binding's children
			var moduleName string
			var moduleTypeName string
			for j := 0; j < int(child.NamedChildCount()); j++ {
				nameChild := child.NamedChild(j)
				if nameChild.Type() == "module_name" {
					moduleName = nameChild.Content(content)
				}
				// The module type constraint appears as module_type_path
				if nameChild.Type() == "module_type_path" || nameChild.Type() == "module_type_name" {
					moduleTypeName = nameChild.Content(content)
				}
			}

			if moduleTypeName != "" && moduleName != "" {
				childID := moduleName
				relationships = append(relationships, &db.TypeHierarchy{
					ChildID:      childID,
					ParentID:     moduleTypeName,
					Relationship: "implements",
				})
			}
			break
		}
	})

	return relationships
}

// Helper: walk tree and call callback for each node
// Helper: extract type name from various node types
func (h *HierarchyIndexer) getTypeName(node *sitter.Node, content []byte) string {
	if node == nil {
		return ""
	}

	switch node.Type() {
	case "identifier", "type_identifier":
		return node.Content(content)
	case "generic_name", "generic_type":
		// Get just the base type name, not the generic params
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			return nameNode.Content(content)
		}
		// Fallback: get first child
		if node.NamedChildCount() > 0 {
			return node.NamedChild(0).Content(content)
		}
	case "qualified_name", "scoped_type_identifier":
		// Return full qualified name
		return node.Content(content)
	case "type":
		// Recurse into type node
		if node.NamedChildCount() > 0 {
			return h.getTypeName(node.NamedChild(0), content)
		}
	}

	// Fallback: return raw content
	return node.Content(content)
}

// Helper: check if name starts with 'I' (interface naming convention)
func startsWithI(name string) bool {
	return len(name) > 1 && name[0] == 'I' && name[1] >= 'A' && name[1] <= 'Z'
}

// IndexHierarchyForFile is a convenience method for single-file indexing
func (h *HierarchyIndexer) IndexHierarchyForFile(ctx context.Context, file FileInfo) (int, error) {
	return h.IndexHierarchyTreeSitter(ctx, file)
}

// Ensure we can measure time for benchmarking if needed
var _ = time.Now
