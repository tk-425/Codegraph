package ignore

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Default patterns to always ignore
var DefaultPatterns = []string{
	// Dependencies
	"node_modules",
	".venv",
	"venv",
	"env",
	"vendor",
	"Pods",
	"Carthage",
	"__pycache__",
	"*.egg-info",
	"dist",
	"build",
	"_build", // OCaml/Dune
	".build",
	".tox",

	// AI/Agent tools
	".claude",
	".codex",
	".codegraph",
	".agent",
	".gemini",

	// Version control
	".git",
	".hg",
	".svn",

	// IDEs
	".vscode",
	".idea",
	".vim",
	".emacs.d",
	"*.swp",
	"*.swo",
	"*~",
	".DS_Store",

	// Build artifacts
	"*.o",
	"*.a",
	"*.so",
	"*.dylib",
	"*.dll",
	"*.exe",
	".gradle",
	"target",

	// Testing
	".pytest_cache",
	".coverage",
	"coverage",
	"htmlcov",

	// Temporary files
	".tmp",
	"tmp",
	".cache",

	// Lock files
	"package-lock.json",
	"yarn.lock",
	"Pipfile.lock",
	"poetry.lock",
}

// Matcher handles ignore pattern matching
type Matcher struct {
	patterns []string
}

// NewMatcher creates a new ignore pattern matcher
func NewMatcher(cgignorePath string) *Matcher {
	m := &Matcher{
		patterns: append([]string{}, DefaultPatterns...),
	}

	// Load custom patterns from .cgignore if it exists
	if cgignorePath != "" {
		m.loadCGIgnore(cgignorePath)
	}

	return m
}

// loadCGIgnore loads patterns from a .cgignore file
func (m *Matcher) loadCGIgnore(path string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m.patterns = append(m.patterns, line)
	}
}

// ShouldIgnore checks if a path should be ignored
func (m *Matcher) ShouldIgnore(path string) bool {
	// Get the base name and all path components
	base := filepath.Base(path)
	parts := strings.Split(filepath.ToSlash(path), "/")

	for _, pattern := range m.patterns {
		// Check if any path component matches the pattern
		for _, part := range parts {
			if matchPattern(pattern, part) {
				return true
			}
		}
		// Also check the full path
		if matchPattern(pattern, path) || matchPattern(pattern, base) {
			return true
		}
	}

	return false
}

// matchPattern performs simple glob matching
func matchPattern(pattern, name string) bool {
	// Handle exact match
	if pattern == name {
		return true
	}

	// Handle wildcard prefix (*.ext)
	if strings.HasPrefix(pattern, "*") {
		suffix := pattern[1:]
		return strings.HasSuffix(name, suffix)
	}

	// Handle wildcard suffix (name*)
	if strings.HasSuffix(pattern, "*") {
		prefix := pattern[:len(pattern)-1]
		return strings.HasPrefix(name, prefix)
	}

	return false
}

// GetPatterns returns all active patterns
func (m *Matcher) GetPatterns() []string {
	return m.patterns
}

// CreateDefaultCGIgnore creates a default .cgignore file
func CreateDefaultCGIgnore(dir string) error {
	path := filepath.Join(dir, ".cgignore")

	content := `# CodeGraph Ignore File
# Patterns listed here will be excluded from indexing.
# Uses glob-style matching (like .gitignore).

# Add custom patterns below:
# test/
# *_test.go
# *.generated.go
`

	return os.WriteFile(path, []byte(content), 0644)
}
