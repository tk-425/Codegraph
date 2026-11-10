package lsp

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

// PathToURI returns the file URI of path, percent-encoding spaces, '#', '?'
// and non-ASCII characters. A relative path is made absolute first.
func PathToURI(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}

// URIToPath decodes a local file URI into a filesystem path.
func URIToPath(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("invalid file URI %q: %w", uri, err)
	}
	if u.Scheme != "file" {
		return "", fmt.Errorf("not a file URI: %q", uri)
	}
	if u.Host != "" && u.Host != "localhost" {
		return "", fmt.Errorf("file URI on remote host: %q", uri)
	}
	return filepath.Clean(filepath.FromSlash(u.Path)), nil
}

// Root maps between project paths and the URIs a server uses. A server may
// answer through the resolved form of a symlinked root (/private/var for
// /var); Path rebases such answers onto the scanned root so they compare
// equal to indexed paths.
type Root struct {
	path string
	real string
}

// NewRoot returns the mapping for the project rooted at path.
func NewRoot(path string) Root {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		real = abs
	}
	return Root{path: abs, real: real}
}

// URI is the root URI sent at initialization.
func (r Root) URI() string { return PathToURI(r.path) }

// Path decodes uri and rebases a path under the resolved root onto the
// scanned root.
func (r Root) Path(uri string) (string, error) {
	path, err := URIToPath(uri)
	if err != nil {
		return "", err
	}
	if r.real != r.path {
		if rest, ok := strings.CutPrefix(path, r.real); ok && (rest == "" || rest[0] == filepath.Separator) {
			return r.path + rest, nil
		}
	}
	return path, nil
}
