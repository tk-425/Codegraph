package indexer

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/tk-425/Codegraph/internal/ignore"
	"github.com/tk-425/Codegraph/internal/lsp/adapters"
)

// FileInfo represents a discovered source file
type FileInfo struct {
	Path     string
	Language string
	RelPath  string
}

// Scanner discovers source files in a project
type Scanner struct {
	rootPath string
	ignore   *ignore.Matcher
}

// NewScanner creates a new file scanner
func NewScanner(rootPath string, ignorePath string) *Scanner {
	return &Scanner{
		rootPath: rootPath,
		ignore:   ignore.NewMatcher(ignorePath),
	}
}

// Scan discovers all source files in the project
func (s *Scanner) Scan() ([]FileInfo, error) {
	var files []FileInfo

	err := filepath.Walk(s.rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Get relative path
		relPath, _ := filepath.Rel(s.rootPath, path)

		// Skip ignored paths
		if s.ignore.ShouldIgnore(relPath) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Check if supported extension
		ext := strings.ToLower(filepath.Ext(path))
		language := adapters.LanguageFromExtension(ext)
		if language == "" {
			return nil
		}

		files = append(files, FileInfo{
			Path:     path,
			Language: language,
			RelPath:  relPath,
		})

		return nil
	})

	return files, err
}

// GroupByLanguage groups files by their language
func GroupByLanguage(files []FileInfo) map[string][]FileInfo {
	groups := make(map[string][]FileInfo)
	for _, f := range files {
		groups[f.Language] = append(groups[f.Language], f)
	}
	return groups
}

// DetectedLanguages returns unique languages from files
func DetectedLanguages(files []FileInfo) []string {
	seen := make(map[string]bool)
	var languages []string
	for _, f := range files {
		if !seen[f.Language] {
			seen[f.Language] = true
			languages = append(languages, f.Language)
		}
	}
	return languages
}
