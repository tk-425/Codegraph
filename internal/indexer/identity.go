package indexer

import (
	"path/filepath"
	"regexp"
	"strings"
)

// symbolKey is the canonical identity of a local symbol within a snapshot:
// language, project-relative file, complete lexical ownership and name.
// A Go method's receiver type is its owner. The ID encoding is versioned with
// the index format (db.FormatVersion); no reader parses it.
type symbolKey struct {
	Language string
	RelPath  string
	Owners   []string
	Name     string
}

// ID encodes the key as "<language>:<relpath>#<owner>.<owner>.<name>".
func (k symbolKey) ID() string {
	qualified := strings.Join(append(append([]string(nil), k.Owners...), k.Name), ".")
	return k.Language + ":" + filepath.ToSlash(k.RelPath) + "#" + qualified
}

// Scope is the stored scope column: the owner chain joined by dots.
func (k symbolKey) Scope() string {
	return strings.Join(k.Owners, ".")
}

// goMethodName matches gopls method names such as "(T).Run", "(*T).Run" and
// "(*T[K]).Run".
var goMethodName = regexp.MustCompile(`^\(\*?([A-Za-z_][A-Za-z0-9_]*)(?:\[[^\]]*\])?\)\.(.+)$`)

// splitGoReceiver turns an LSP Go method name into its receiver type and
// method name, so LSP and syntax extraction agree on name and owner.
func splitGoReceiver(name string) (receiver, method string, ok bool) {
	m := goMethodName.FindStringSubmatch(name)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// goReceiverType strips pointer and type arguments from a receiver type
// expression: "*T", "T[K]" and "*T[K, V]" all name T.
func goReceiverType(expr string) string {
	expr = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(expr), "*"))
	if i := strings.IndexByte(expr, '['); i >= 0 {
		expr = expr[:i]
	}
	return strings.TrimSpace(expr)
}
