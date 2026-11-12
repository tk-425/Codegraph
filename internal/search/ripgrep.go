package search

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/tk-425/Codegraph/internal/indexer"
)

// RipgrepTier searches scanner-eligible source files for literal text.
type RipgrepTier struct {
	rootPath string
}

func NewRipgrepTier(rootPath string) *RipgrepTier {
	return &RipgrepTier{rootPath: rootPath}
}

func (r *RipgrepTier) Name() string { return "ripgrep" }

func (r *RipgrepTier) Search(ctx context.Context, opts SearchOptions) ([]SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if opts.ExactMatch || opts.Kind != "" {
		return nil, &TierSkip{Tier: r.Name(), Reason: "text matches cannot satisfy --exact or --kind symbol filters"}
	}
	scanner, err := indexer.NewScanner(r.rootPath, filepath.Join(r.rootPath, ".codegraph", ".cgignore"))
	if err != nil {
		return nil, err
	}
	files, err := scanner.Scan(ctx)
	if err != nil {
		return nil, err
	}
	var eligible []indexer.FileInfo
	for _, file := range files {
		if len(opts.Languages) == 0 || slices.Contains(opts.Languages, file.Language) {
			eligible = append(eligible, file)
		}
	}
	var results []SearchResult
	for start := 0; start < len(eligible); {
		// Bound path arguments conservatively; split on E2BIG as well, since
		// the environment and pattern also consume the OS argument budget.
		end, size := start, 0
		for end < len(eligible) && (end == start || size+len(eligible[end].Path)+1 <= 32*1024) {
			size += len(eligible[end].Path) + 1
			end++
		}
		batch, err := r.searchFiles(ctx, opts, eligible[start:end])
		if err != nil {
			return nil, err
		}
		results = append(results, batch...)
		if opts.Limit > 0 && len(results) >= opts.Limit {
			return results[:opts.Limit], nil
		}
		start = end
	}
	return results, nil
}

func (r *RipgrepTier) searchFiles(ctx context.Context, opts SearchOptions, files []indexer.FileInfo) ([]SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	args := []string{"--no-config", "--json", "--fixed-strings", "--sort", "path"}
	if strings.Contains(opts.Query, "\n") {
		args = append(args, "--multiline")
	}
	args = append(args, "-e", opts.Query, "--")
	languages := make(map[string]string, len(files))
	for _, file := range files {
		args = append(args, file.Path)
		languages[file.Path] = file.Language
	}
	cmd := exec.CommandContext(ctx, "rg", args...)
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, syscall.E2BIG) && len(files) > 1 {
			middle := len(files) / 2
			left, err := r.searchFiles(ctx, opts, files[:middle])
			if err != nil {
				return nil, err
			}
			right, err := r.searchFiles(ctx, opts, files[middle:])
			return append(left, right...), err
		}
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			if exitErr != nil {
				return nil, fmt.Errorf("ripgrep: %w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
			}
			return nil, err
		}
	}
	return r.parseOutput(output, opts, languages)
}

// rgText is ripgrep's native representation for UTF-8 or base64-encoded data.
type rgText struct {
	Text  *string `json:"text"`
	Bytes *string `json:"bytes"`
}

func (t rgText) value() (string, error) {
	if t.Text != nil {
		return *t.Text, nil
	}
	if t.Bytes != nil {
		value, err := base64.StdEncoding.DecodeString(*t.Bytes)
		return string(value), err
	}
	return "", errors.New("ripgrep data has neither text nor bytes")
}

func (r *RipgrepTier) parseOutput(output []byte, opts SearchOptions, languages map[string]string) ([]SearchResult, error) {
	var results []SearchResult
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		var message struct {
			Type string `json:"type"`
			Data struct {
				Path       rgText `json:"path"`
				Lines      rgText `json:"lines"`
				LineNumber *int   `json:"line_number"`
				Submatches []struct {
					Start int `json:"start"`
				} `json:"submatches"`
			} `json:"data"`
		}
		if err := decoder.Decode(&message); err != nil {
			if errors.Is(err, io.EOF) {
				return results, nil
			}
			return nil, fmt.Errorf("decode ripgrep output: %w", err)
		}
		if message.Type != "match" {
			continue
		}
		file, err := message.Data.Path.value()
		if err != nil {
			return nil, err
		}
		content, err := message.Data.Lines.value()
		if err != nil {
			return nil, err
		}
		language, eligible := languages[file]
		if !eligible || message.Data.LineNumber == nil || len(message.Data.Submatches) == 0 {
			return nil, errors.New("ripgrep returned a match without an eligible path or position")
		}
		results = append(results, SearchResult{
			Name: opts.Query, Kind: "match", File: file,
			Line: *message.Data.LineNumber, Column: message.Data.Submatches[0].Start + 1,
			Language: language, Source: "ripgrep", Score: 0.5, Context: strings.TrimSpace(content),
		})
	}
}
