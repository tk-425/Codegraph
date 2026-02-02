package adapters

import (
	"github.com/tk-425/Codegraph/internal/lsp"
)

// Adapter defines the interface for language-specific LSP customizations
type Adapter interface {
	// Language returns the language identifier
	Language() string

	// Extensions returns file extensions for this language
	Extensions() []string

	// NormalizeSymbol adjusts symbol data for language-specific quirks
	NormalizeSymbol(sym *lsp.DocumentSymbol) *lsp.DocumentSymbol

	// FileURI converts a file path to a URI for this language's LSP
	FileURI(path string) string
}

// BaseAdapter provides common functionality for all adapters
type BaseAdapter struct {
	lang       string
	extensions []string
}

func (a *BaseAdapter) Language() string {
	return a.lang
}

func (a *BaseAdapter) Extensions() []string {
	return a.extensions
}

func (a *BaseAdapter) NormalizeSymbol(sym *lsp.DocumentSymbol) *lsp.DocumentSymbol {
	return sym // Default: no normalization
}

func (a *BaseAdapter) FileURI(path string) string {
	return "file://" + path
}

// LanguageFromExtension returns the language for a file extension
func LanguageFromExtension(ext string) string {
	switch ext {
	case ".go":
		return "go"
	case ".py", ".pyw":
		return "python"
	case ".ts", ".mts", ".cts":
		return "typescript"
	case ".tsx", ".jsx":
		return "typescriptreact"
	case ".js", ".mjs", ".cjs":
		return "typescript" // Use typescript LSP for JS too
	case ".java":
		return "java"
	case ".swift":
		return "swift"
	case ".rs":
		return "rust"
	case ".ml", ".mli":
		return "ocaml"
	default:
		return ""
	}
}

// SupportedExtensions returns all supported file extensions
func SupportedExtensions() []string {
	return []string{
		".go",
		".py", ".pyw",
		".ts", ".tsx", ".mts", ".cts",
		".js", ".jsx", ".mjs", ".cjs",
		".java",
		".swift",
		".rs",
		".ml", ".mli",
	}
}
