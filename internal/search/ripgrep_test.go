package search

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func textSearchProject(t *testing.T, files map[string]string, ignore string) string {
	t.Helper()
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("real ripgrep is unavailable; text integration not verified")
	}
	root := t.TempDir()
	filesWithIgnore := map[string]string{".codegraph/.cgignore": ignore}
	for path, content := range files {
		filesWithIgnore[path] = content
	}
	for name, content := range filesWithIgnore {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestRipgrepUsesScannerEligibility(t *testing.T) {
	root := textSearchProject(t, map[string]string{
		".git/HEAD":       "ref: refs/heads/main\n",
		".gitignore":      "included.go\n",
		".hidden.go":      "// EligibilityMarker\n",
		"included.go":     "// EligibilityMarker\n",
		"excluded.go":     "// EligibilityMarker\n",
		"ignored/drop.go": "// EligibilityMarker\n",
		"ignored/keep.go": "// EligibilityMarker\n",
		"note.txt":        "EligibilityMarker\n",
		"a.ts":            "// EligibilityMarker\n",
		"b.tsx":           "// EligibilityMarker\n",
		"c.js":            "// EligibilityMarker\n",
	}, "excluded.go\nignored/*\n!ignored/keep.go\n")
	for _, tc := range []struct {
		language string
		limit    int
		want     []string
	}{
		{"go", 0, []string{".hidden.go", "ignored/keep.go", "included.go"}},
		{"go", 1, []string{".hidden.go"}},
		{"typescript", 0, []string{"a.ts", "c.js"}},
		{"typescriptreact", 0, []string{"b.tsx"}},
		{"javascript", 0, nil}, // Follow the scanner's stored language IDs, not rg aliases.
		{"unknown", 0, nil},
	} {
		t.Run(tc.language+"/limit="+strconv.Itoa(tc.limit), func(t *testing.T) {
			results, err := NewRipgrepTier(root).Search(context.Background(), SearchOptions{Query: "EligibilityMarker", Languages: []string{tc.language}, Limit: tc.limit})
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, result := range results {
				name := result.File
				if filepath.IsAbs(name) {
					name, err = filepath.Rel(root, name)
					if err != nil {
						t.Fatal(err)
					}
				}
				names = append(names, filepath.ToSlash(name))
				if result.Language != tc.language {
					t.Fatalf("language=%q, want %q", result.Language, tc.language)
				}
			}
			if !reflect.DeepEqual(names, tc.want) {
				t.Fatalf("eligible files=%v, want %v", names, tc.want)
			}
		})
	}
}

func TestRipgrepUnusualPathsAndData(t *testing.T) {
	name := "space #é𝔘:line\nbreak.go"
	prefix := strings.Repeat("x", 70*1024) + " " // Exceeds bufio.Scanner's default token limit.
	root := textSearchProject(t, map[string]string{name: prefix + "UnusualMarker\xff\n"}, "")
	results, err := NewRipgrepTier(root).Search(context.Background(), SearchOptions{Query: "UnusualMarker"})
	if err != nil || len(results) != 1 {
		t.Fatalf("unusual path/data results=%+v err=%v", results, err)
	}
	result := results[0]
	if result.File != filepath.Join(root, name) || result.Line != 1 || result.Column != len(prefix)+1 || !strings.Contains(result.Context, "UnusualMarker\xff") {
		t.Fatalf("path/position/content lost: file=%q line=%d column=%d", result.File, result.Line, result.Column)
	}
}

func TestRipgrepBatchesAndLimits(t *testing.T) {
	files := map[string]string{}
	for i := 0; i < 300; i++ {
		files[fmt.Sprintf("%03d-%s.go", i, strings.Repeat("p", 100))] = "// BatchMarker\n// BatchMarker\n"
	}
	root := textSearchProject(t, files, "")
	for _, limit := range []int{0, 1, 301} {
		results, err := NewRipgrepTier(root).Search(context.Background(), SearchOptions{Query: "BatchMarker", Limit: limit})
		want := 600
		if limit > 0 {
			want = limit
		}
		if err != nil || len(results) != want {
			t.Fatalf("limit=%d count=%d want=%d err=%v", limit, len(results), want, err)
		}
		for i, result := range results {
			name := fmt.Sprintf("%03d-%s.go", i/2, strings.Repeat("p", 100))
			if result.File != filepath.Join(root, name) || result.Line != i%2+1 {
				t.Fatalf("unstable batch order at %d: file=%q line=%d", i, result.File, result.Line)
			}
		}
	}
}

func TestRipgrepEmptyEligibilityDoesNotRunProcess(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".codegraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".codegraph", ".cgignore"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir()) // No rg executable or stdin fallback is available.
	results, err := NewRipgrepTier(root).Search(context.Background(), SearchOptions{Query: "anything"})
	if err != nil || len(results) != 0 {
		t.Fatalf("empty eligibility started a process: results=%+v err=%v", results, err)
	}
}

func TestRipgrepConfigurationFailuresAndCancellation(t *testing.T) {
	root := textSearchProject(t, map[string]string{
		"source.go": "// CaseMarker\n",
		"rg.conf":   "--ignore-case\n",
	}, "")
	t.Setenv("RIPGREP_CONFIG_PATH", filepath.Join(root, "rg.conf"))
	results, err := NewRipgrepTier(root).Search(context.Background(), SearchOptions{Query: "casemarker"})
	if err != nil || len(results) != 0 {
		t.Fatalf("external rg config changed matching: results=%+v err=%v", results, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewRipgrepTier(root).Search(ctx, SearchOptions{Query: "CaseMarker"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled search err=%v", err)
	}
	if _, err := NewRipgrepTier(t.TempDir()).Search(context.Background(), SearchOptions{Query: "CaseMarker"}); err == nil {
		t.Fatal("missing ignore configuration became empty success")
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := NewRipgrepTier(root).Search(context.Background(), SearchOptions{Query: "CaseMarker"}); err == nil {
		t.Fatal("eligible files with missing rg became empty success")
	}
}

func TestRipgrepLiteralQueries(t *testing.T) {
	root := textSearchProject(t, map[string]string{
		"source.go": "// marker[abc] marker.thing marker%value marker_under marker\\value -marker\n// markerXthing markeravalue\n// CaseMarker\n",
	}, "")
	for _, query := range []string{"marker[abc]", "marker.thing", "marker%value", "marker_under", `marker\value`, "-marker"} {
		t.Run(query, func(t *testing.T) {
			results, err := NewRipgrepTier(root).Search(context.Background(), SearchOptions{Query: query})
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 1 || results[0].Line != 1 || results[0].Kind != "match" || results[0].ID != "" || results[0].Source != "ripgrep" {
				t.Fatalf("literal query %q: %+v", query, results)
			}
		})
	}
	results, err := NewRipgrepTier(root).Search(context.Background(), SearchOptions{Query: "casemarker"})
	if err != nil || len(results) != 0 {
		t.Fatalf("ripgrep case defaults changed: results=%+v err=%v", results, err)
	}
}
