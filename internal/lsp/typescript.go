package lsp

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/tk-425/Codegraph/internal/config"
)

const (
	defaultTypeScriptCommand = "typescript-language-server"
	defaultTypeScriptArg     = "--stdio"
	nativeTypeScriptArg      = "--lsp"
)

type typeScriptServer struct {
	command string
	args    []string
	native  bool
}

// resolveTypeScriptServer selects the TypeScript server for a project. The
// generated wrapper configuration is automatic; any other command or args
// remain an explicit project override.
func resolveTypeScriptServer(cfg *config.Config, projectRoot, language string) (typeScriptServer, error) {
	configured, ok := cfg.LSP[language]
	if !ok && language == "typescriptreact" {
		configured, ok = cfg.LSP["typescript"]
	}
	if !ok {
		return typeScriptServer{}, fmt.Errorf("no LSP configuration for language: %s", language)
	}

	if !isAutomaticTypeScriptConfig(configured) {
		return typeScriptServer{command: configured.Command, args: append([]string(nil), configured.Args...)}, nil
	}

	version, executable, err := discoverLocalTypeScript(projectRoot)
	if err != nil || version.Major < 7 {
		return typeScriptServer{command: configured.Command, args: append([]string(nil), configured.Args...)}, nil
	}
	if executable == "" {
		return typeScriptServer{command: configured.Command, args: append([]string(nil), configured.Args...)}, nil
	}

	return typeScriptServer{command: executable, args: []string{nativeTypeScriptArg, defaultTypeScriptArg}, native: true}, nil
}

func isAutomaticTypeScriptConfig(cfg config.LSPConfig) bool {
	return cfg.Command == defaultTypeScriptCommand && len(cfg.Args) == 1 && cfg.Args[0] == defaultTypeScriptArg
}

type typeScriptVersion struct {
	Major int
	Minor int
	Patch int
}

var versionPattern = regexp.MustCompile(`^(?:v)?(\d+)(?:\.(\d+))?(?:\.(\d+))?`)

func discoverLocalTypeScript(projectRoot string) (typeScriptVersion, string, error) {
	packageRoot := filepath.Join(projectRoot, "node_modules", "typescript")
	data, err := os.ReadFile(filepath.Join(packageRoot, "package.json"))
	if err != nil {
		return typeScriptVersion{}, "", err
	}

	var manifest struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return typeScriptVersion{}, "", fmt.Errorf("parse TypeScript package metadata: %w", err)
	}

	version, err := parseTypeScriptVersion(manifest.Version)
	if err != nil {
		return typeScriptVersion{}, "", err
	}

	candidates := []string{
		filepath.Join(projectRoot, "node_modules", ".bin", "tsc"),
		filepath.Join(packageRoot, "bin", "tsc"),
		filepath.Join(packageRoot, "bin", "tsc.js"),
	}
	for _, candidate := range candidates {
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return version, candidate, nil
		}
	}
	return version, "", nil
}

func parseTypeScriptVersion(raw string) (typeScriptVersion, error) {
	match := versionPattern.FindString(strings.TrimSpace(raw))
	if match == "" {
		return typeScriptVersion{}, fmt.Errorf("invalid TypeScript version %q", raw)
	}
	parts := strings.Split(strings.TrimPrefix(match, "v"), ".")
	values := [3]int{}
	for i, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil {
			return typeScriptVersion{}, fmt.Errorf("invalid TypeScript version %q: %w", raw, err)
		}
		values[i] = value
	}
	return typeScriptVersion{Major: values[0], Minor: values[1], Patch: values[2]}, nil
}

func projectRootFromURI(rootURI string) string {
	parsed, err := url.Parse(rootURI)
	if err == nil && parsed.Path != "" {
		return parsed.Path
	}
	return strings.TrimPrefix(rootURI, "file://")
}
