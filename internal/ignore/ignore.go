package ignore

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	goignore "github.com/Sriram-PR/go-ignore"
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

// Matcher handles ignore pattern matching.
type Matcher struct {
	matcher     *goignore.Matcher
	patterns    []string
	hasNegation bool
}

// NewMatcher creates a matcher that evaluates .cgignore using gitignore-style semantics.
func NewMatcher(cgignorePath string) (*Matcher, error) {
	m := &Matcher{
		matcher:  goignore.New(),
		patterns: append([]string{}, DefaultPatterns...),
	}

	defaultContent := strings.Join(DefaultPatterns, "\n") + "\n"
	if warnings := m.matcher.AddPatterns("", []byte(defaultContent)); len(warnings) > 0 {
		return nil, formatWarnings("built-in ignore patterns", warnings)
	}

	if cgignorePath == "" {
		return m, nil
	}

	content, err := os.ReadFile(cgignorePath)
	if err != nil {
		return nil, fmt.Errorf("read .cgignore: %w", err)
	}

	m.patterns = append(m.patterns, extractPatterns(content)...)
	m.hasNegation = hasNegationPatterns(content)

	if warnings := m.matcher.AddPatterns("", content); len(warnings) > 0 {
		return nil, formatWarnings(cgignorePath, warnings)
	}

	return m, nil
}

// ShouldIgnore checks if a path should be ignored.
func (m *Matcher) ShouldIgnore(path string, isDir bool) bool {
	normalized := normalizePath(path)
	if normalized == "" {
		return false
	}
	return m.matcher.Match(normalized, isDir)
}

// ShouldSkipDir reports whether the walker can prune an ignored directory safely.
func (m *Matcher) ShouldSkipDir(path string) bool {
	return m.ShouldIgnore(path, true) && !m.hasNegation
}

// GetPatterns returns all active patterns.
func (m *Matcher) GetPatterns() []string {
	return append([]string{}, m.patterns...)
}

// CreateDefaultCGIgnore creates a .cgignore file seeded from the project's .gitignore.
func CreateDefaultCGIgnore(codegraphDir, projectRoot string) error {
	path := filepath.Join(codegraphDir, ".cgignore")

	var b strings.Builder
	b.WriteString("# CodeGraph Ignore File\n")
	b.WriteString("# Initialized from project .gitignore during `codegraph init`.\n")
	b.WriteString("# Edit this file to control what CodeGraph indexes.\n")
	b.WriteString("# Rerun `codegraph build` after changes.\n\n")

	gitignorePath := filepath.Join(projectRoot, ".gitignore")
	content, err := os.ReadFile(gitignorePath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("read .gitignore: %w", err)
		}
	} else {
		b.WriteString("# Imported from .gitignore\n")
		b.Write(content)
		if len(content) > 0 && content[len(content)-1] != '\n' {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("# Add CodeGraph-only exclusions below:\n")
	b.WriteString("# test/\n")
	b.WriteString("# *_test.go\n")
	b.WriteString("# *.generated.go\n")

	return os.WriteFile(path, []byte(b.String()), 0644)
}

func extractPatterns(content []byte) []string {
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	patterns := make([]string, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns
}

func hasNegationPatterns(content []byte) bool {
	for _, pattern := range extractPatterns(content) {
		if strings.HasPrefix(pattern, "!") {
			return true
		}
	}
	return false
}

func normalizePath(path string) string {
	normalized := filepath.ToSlash(path)
	normalized = strings.TrimPrefix(normalized, "./")
	if normalized == "." {
		return ""
	}
	return strings.TrimSuffix(normalized, "/")
}

func formatWarnings(source string, warnings []goignore.ParseWarning) error {
	parts := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		parts = append(parts, fmt.Sprintf("line %d: %s (%s)", warning.Line, warning.Message, warning.Pattern))
	}
	return fmt.Errorf("invalid ignore patterns in %s: %s", source, strings.Join(parts, "; "))
}
