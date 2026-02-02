package adapters

import (
	"strings"

	"github.com/tk-425/Codegraph/internal/lsp"
)

// GoAdapter is the adapter for gopls (Go LSP)
type GoAdapter struct {
	BaseAdapter
}

// NewGoAdapter creates a new Go adapter
func NewGoAdapter() *GoAdapter {
	return &GoAdapter{
		BaseAdapter: BaseAdapter{
			lang:       "go",
			extensions: []string{".go"},
		},
	}
}

// NormalizeSymbol adjusts Go-specific symbol data
func (a *GoAdapter) NormalizeSymbol(sym *lsp.DocumentSymbol) *lsp.DocumentSymbol {
	// gopls sometimes includes package prefixes, normalize them
	if strings.Contains(sym.Name, ".") {
		parts := strings.Split(sym.Name, ".")
		sym.Name = parts[len(parts)-1]
	}
	
	return sym
}

// FileURI converts a file path to a URI for gopls
func (a *GoAdapter) FileURI(path string) string {
	// gopls expects file:// URIs
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return "file://" + path
}
