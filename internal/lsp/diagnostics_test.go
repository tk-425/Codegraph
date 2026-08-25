package lsp

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMetadataAndGuidance(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pnpm-lock.yaml"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	executable, docs, ok := Metadata("typescript")
	if !ok || executable == "" || docs == "" {
		t.Fatalf("metadata = %q, %q, %v", executable, docs, ok)
	}
	guidance := Guidance("typescript", root, false)
	if guidance == nil || guidance.Command != "pnpm add -g typescript-language-server typescript" || guidance.Documentation == "" {
		t.Fatalf("guidance = %+v", guidance)
	}
	if Guidance("typescript", root, true) != nil {
		t.Fatal("explicit override should suppress default guidance")
	}
}

func TestPackageManagerDetection(t *testing.T) {
	cases := []struct {
		name string
		file string
		want string
	}{
		{"pnpm", "pnpm-lock.yaml", "pnpm"},
		{"npm", "package-lock.json", "npm"},
		{"yarn", "yarn.lock", "yarn"},
		{"bun", "bun.lockb", "bun"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, tc.file), nil, 0644); err != nil {
				t.Fatal(err)
			}
			if got := PackageManager(root); got != tc.want {
				t.Fatalf("PackageManager = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClassifyFailure(t *testing.T) {
	if got := ClassifyFailure(&os.PathError{Op: "exec", Path: "missing", Err: os.ErrNotExist}); got != MissingExecutable {
		t.Fatalf("missing classification = %q", got)
	}
	if got := ClassifyFailure(errors.New("initialize failed")); got != InitializationFailure {
		t.Fatalf("initialization classification = %q", got)
	}
}
