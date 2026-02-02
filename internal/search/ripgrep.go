package search

import (
	"bufio"
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// RipgrepTier searches using ripgrep as a fallback
type RipgrepTier struct {
	rootPath string
}

// NewRipgrepTier creates a new ripgrep search tier
func NewRipgrepTier(rootPath string) *RipgrepTier {
	return &RipgrepTier{rootPath: rootPath}
}

// Name returns the tier name
func (r *RipgrepTier) Name() string {
	return "ripgrep"
}

// Search uses ripgrep to find matches
func (r *RipgrepTier) Search(ctx context.Context, opts SearchOptions) ([]SearchResult, error) {
	args := []string{
		"--line-number",
		"--column",
		"--no-heading",
		"--color=never",
		"--with-filename",
	}

	// Add language filter using ripgrep type
	for _, lang := range opts.Languages {
		switch lang {
		case "go":
			args = append(args, "--type", "go")
		case "python":
			args = append(args, "--type", "py")
		case "typescript", "javascript":
			args = append(args, "--type", "ts", "--type", "js")
		case "rust":
			args = append(args, "--type", "rust")
		case "java":
			args = append(args, "--type", "java")
		}
	}

	// Word boundary for better matching
	if opts.ExactMatch {
		args = append(args, "--word-regexp")
	}

	args = append(args, opts.Query, r.rootPath)

	cmd := exec.CommandContext(ctx, "rg", args...)
	output, err := cmd.Output()
	if err != nil {
		// ripgrep returns exit code 1 when no matches found
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return []SearchResult{}, nil
		}
		return nil, err
	}

	return r.parseOutput(string(output), opts)
}

// parseOutput parses ripgrep output into SearchResults
func (r *RipgrepTier) parseOutput(output string, opts SearchOptions) ([]SearchResult, error) {
	var results []SearchResult
	scanner := bufio.NewScanner(strings.NewReader(output))

	for scanner.Scan() {
		line := scanner.Text()
		// Format: file:line:column:content
		parts := strings.SplitN(line, ":", 4)
		if len(parts) < 4 {
			continue
		}

		file := parts[0]
		lineNum, _ := strconv.Atoi(parts[1])
		colNum, _ := strconv.Atoi(parts[2])
		content := strings.TrimSpace(parts[3])

		// Make file path relative if possible
		relPath, err := filepath.Rel(r.rootPath, file)
		if err == nil {
			file = relPath
		}

		// Detect language from extension
		ext := filepath.Ext(file)
		lang := extensionToLanguage(ext)

		results = append(results, SearchResult{
			Name:     opts.Query,
			Kind:     "match",
			File:     file,
			Line:     lineNum,
			Column:   colNum,
			Language: lang,
			Source:   "ripgrep",
			Score:    0.5, // Lower score than DB results
			Context:  content,
		})

		// Apply limit
		if opts.Limit > 0 && len(results) >= opts.Limit {
			break
		}
	}

	return results, scanner.Err()
}

func extensionToLanguage(ext string) string {
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".swift":
		return "swift"
	case ".ml", ".mli":
		return "ocaml"
	default:
		return "unknown"
	}
}
