package indexer

import (
	"context"
	"fmt"
	"os"

	"github.com/tk-425/Codegraph/internal/db"
	"github.com/tk-425/Codegraph/internal/lsp"
)

// CallGraphIndexer extracts calls from a language server
type CallGraphIndexer struct {
	db       *db.Manager
	mgr      *lsp.Manager
	rootPath string
	// failures counts failed requests of the latest IndexCallGraph call.
	failures requestFailures
}

// NewCallGraphIndexer creates a new call graph indexer
func NewCallGraphIndexer(dbManager *db.Manager, lspManager *lsp.Manager, rootPath string) *CallGraphIndexer {
	return &CallGraphIndexer{
		db:       dbManager,
		mgr:      lspManager,
		rootPath: rootPath,
	}
}

// IndexCallGraph extracts the calls to every indexed callable of language.
// It asks for incoming calls when the server advertises call hierarchy; the
// client offers no dynamic registration, so the static capability is the
// whole answer (LSP 3.17). Otherwise a reference is a call only when the
// syntax at that position is an invocation. Either way the caller is an
// indexed callable and each call site is kept, so distinct targets at one
// site stay distinct.
func (c *CallGraphIndexer) IndexCallGraph(ctx context.Context, language string) (int, error) {
	c.failures = requestFailures{}
	// Get LSP client for this language
	client, err := c.mgr.GetClient(ctx, language)
	if err != nil {
		return 0, fmt.Errorf("failed to get LSP client: %w", err)
	}

	// Clear existing call graph for this language to avoid duplicates
	if err := c.db.ClearCalls(language); err != nil {
		return 0, fmt.Errorf("failed to clear existing calls: %w", err)
	}

	// Get all function symbols from database
	symbols, err := c.db.GetFunctionSymbols(language)
	if err != nil {
		return 0, fmt.Errorf("failed to get function symbols: %w", err)
	}

	pass := &callPass{
		db:          c.db,
		language:    language,
		root:        lsp.NewRoot(c.rootPath),
		lines:       lsp.NewSourceLines(),
		enc:         client.PositionEncoding(),
		symbols:     map[string]fileSymbols{},
		invocations: map[string]map[position]bool{},
	}
	openedFiles := make(map[string]bool)
	defer func() {
		for fileURI := range openedFiles {
			client.DidCloseTextDocument(fileURI)
		}
	}()

	useHierarchy := lsp.Supports(client.Capabilities().CallHierarchyProvider)
	callCount := 0
	for _, sym := range symbols {
		fileURI := lsp.PathToURI(sym.File)

		// Open file if not already opened
		if !openedFiles[fileURI] {
			content, err := os.ReadFile(sym.File)
			if err != nil {
				continue
			}
			if err := client.DidOpenTextDocument(fileURI, language, string(content)); err != nil {
				c.failures.add(client, err)
				continue
			}
			openedFiles[fileURI] = true
		}

		pos := pass.lines.ToLSP(pass.enc, sym.File, sym.Line, sym.Column)
		if useHierarchy {
			stored, err := pass.incoming(ctx, client, fileURI, pos, sym)
			callCount += stored
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return callCount, ctxErr
				}
				c.failures.add(client, err)
			}
			continue
		}
		refs, err := client.References(ctx, fileURI, pos, false)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return callCount, ctxErr
			}
			c.failures.add(client, err)
			continue
		}
		for _, ref := range refs {
			path, err := pass.root.Path(ref.URI)
			if err != nil {
				continue
			}
			line, column := pass.lines.FromLSP(pass.enc, path, ref.Range.Start)
			stored, err := pass.reference(ctx, sym, path, line, column)
			if err != nil {
				return callCount, err
			}
			if stored {
				callCount++
			}
		}
	}

	return callCount, nil
}

// callPass holds what one call-graph pass learns about each file.
type callPass struct {
	db          *db.Manager
	language    string
	root        lsp.Root
	lines       *lsp.SourceLines
	enc         lsp.PositionEncoding
	symbols     map[string]fileSymbols
	invocations map[string]map[position]bool
}

func (p *callPass) fileSymbols(path string) (fileSymbols, error) {
	if symbols, ok := p.symbols[path]; ok {
		return symbols, nil
	}
	symbols, err := loadFileSymbols(p.db, path)
	if err != nil {
		return nil, err
	}
	p.symbols[path] = symbols
	return symbols, nil
}

// reference stores the reference to callee at path:line:column as a call
// when it is a validated invocation inside an indexed callable. It reports
// whether a call was stored.
func (p *callPass) reference(ctx context.Context, callee db.Symbol, path string, line, column int) (bool, error) {
	if path == callee.File && line == callee.Line && column == callee.Column {
		return false, nil // the declaration itself
	}
	symbols, err := p.fileSymbols(path)
	if err != nil {
		return false, err
	}
	if len(symbols) == 0 {
		return false, nil // outside the indexed project
	}
	site := &db.Unresolved{Kind: "call", Language: p.language, TargetName: callee.Name, File: path, Line: line, Column: column}

	sites, known := p.invocations[path]
	if !known {
		var checkable bool
		sites, checkable, err = invocationSites(ctx, path, p.language)
		if err != nil {
			return false, err
		}
		if !checkable {
			sites = nil
		}
		p.invocations[path] = sites
	}
	if sites == nil {
		// No syntax to prove an invocation: recorded, never a call.
		site.Reason = reasonUnvalidated
		return false, p.db.InsertUnresolved(site)
	}
	if !sites[position{line, column}] {
		return false, nil // a reference that is not a call (I06)
	}

	caller := symbols.enclosing(line, column, callableKinds)
	if caller == nil {
		site.Reason = reasonUnresolved
		return false, p.db.InsertUnresolved(site)
	}
	err = p.db.InsertCall(&db.Call{
		CallerID: caller.ID,
		CalleeID: callee.ID,
		File:     path,
		Line:     line,
		Column:   column,
	})
	return err == nil, nil
}

// incoming stores the server's incoming calls to callee, one per call site.
// It returns how many calls it stored.
func (p *callPass) incoming(ctx context.Context, client *lsp.Client, uri string, pos lsp.Position, callee db.Symbol) (int, error) {
	items, err := client.PrepareCallHierarchy(ctx, uri, pos)
	if err != nil {
		return 0, err
	}
	stored := 0
	for _, item := range items {
		calls, err := client.IncomingCalls(ctx, item)
		if err != nil {
			return stored, err
		}
		for _, in := range calls {
			path, err := p.root.Path(in.From.URI)
			if err != nil {
				continue
			}
			symbols, err := p.fileSymbols(path)
			if err != nil {
				return stored, err
			}
			if len(symbols) == 0 {
				continue // a caller outside the indexed project
			}
			line, column := p.lines.FromLSP(p.enc, path, in.From.SelectionRange.Start)
			caller := symbols.named(line, column)
			if caller == nil || !callableKinds[caller.Kind] {
				caller = symbols.enclosing(line, column, callableKinds)
			}
			for _, r := range in.FromRanges {
				siteLine, siteColumn := p.lines.FromLSP(p.enc, path, r.Start)
				if caller == nil {
					site := &db.Unresolved{Kind: "call", Language: p.language, TargetName: callee.Name, File: path, Line: siteLine, Column: siteColumn, Reason: reasonUnresolved}
					if err := p.db.InsertUnresolved(site); err != nil {
						return stored, err
					}
					continue
				}
				if err := p.db.InsertCall(&db.Call{CallerID: caller.ID, CalleeID: callee.ID, File: path, Line: siteLine, Column: siteColumn}); err == nil {
					stored++
				}
			}
		}
	}
	return stored, nil
}
