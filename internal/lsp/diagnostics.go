package lsp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DiagnosticCategory identifies the language-server condition a diagnostic describes.
type DiagnosticCategory string

const (
	MissingExecutable     DiagnosticCategory = "missing_executable"
	InitializationFailure DiagnosticCategory = "initialization_failure"
	ServerLog             DiagnosticCategory = "server_log"
)

// DiagnosticSeverity is the severity a diagnostic carries.
type DiagnosticSeverity string

const (
	SeverityError   DiagnosticSeverity = "error"
	SeverityWarning DiagnosticSeverity = "warning"
)

// SeverityForMessageType maps a protocol message type to a diagnostic severity.
// Only Error and Warning are retained; retained reports whether the message is kept.
// This is the sole protocol-to-severity mapping in the codebase.
func SeverityForMessageType(t MessageType) (severity DiagnosticSeverity, retained bool) {
	switch t {
	case MessageTypeError:
		return SeverityError, true
	case MessageTypeWarning:
		return SeverityWarning, true
	default:
		return "", false
	}
}

// InstallationGuidance describes a safe remediation path.
type InstallationGuidance struct {
	Command       string `json:"command,omitempty"`
	Documentation string `json:"documentation_url,omitempty"`
}

// LSPDiagnostic is a non-fatal build-time LSP warning.
type LSPDiagnostic struct {
	Language   string                `json:"language"`
	Executable string                `json:"executable"`
	Category   DiagnosticCategory    `json:"category"`
	Severity   DiagnosticSeverity    `json:"severity"`
	Reason     string                `json:"reason"`
	Guidance   *InstallationGuidance `json:"guidance,omitempty"`
	Overridden bool                  `json:"explicit_override"`
}

type languageMetadata struct {
	Executable string
	Docs       string
	Commands   map[string]string
}

var metadata = map[string]languageMetadata{
	"go":              {Executable: "gopls", Docs: "https://go.dev/gopls/", Commands: map[string]string{"": "go install golang.org/x/tools/gopls@latest"}},
	"python":          {Executable: "pyright-langserver", Docs: "https://github.com/microsoft/pyright", Commands: map[string]string{"npm": "npm install -g pyright", "pnpm": "pnpm add -g pyright", "yarn": "yarn global add pyright", "bun": "bun add -g pyright"}},
	"typescript":      {Executable: "typescript-language-server", Docs: "https://github.com/typescript-language-server/typescript-language-server", Commands: map[string]string{"npm": "npm install -g typescript-language-server typescript", "pnpm": "pnpm add -g typescript-language-server typescript", "yarn": "yarn global add typescript-language-server typescript", "bun": "bun add -g typescript-language-server typescript"}},
	"typescriptreact": {Executable: "typescript-language-server", Docs: "https://github.com/typescript-language-server/typescript-language-server", Commands: map[string]string{"npm": "npm install -g typescript-language-server typescript", "pnpm": "pnpm add -g typescript-language-server typescript", "yarn": "yarn global add typescript-language-server typescript", "bun": "bun add -g typescript-language-server typescript"}},
	"rust":            {Executable: "rust-analyzer", Docs: "https://rust-analyzer.github.io/", Commands: map[string]string{"": "rustup component add rust-analyzer"}},
	"java":            {Executable: "jdtls", Docs: "https://github.com/eclipse-jdtls/eclipse.jdt.ls"},
	"swift":           {Executable: "sourcekit-lsp", Docs: "https://github.com/apple/sourcekit-lsp"},
	"ocaml":           {Executable: "ocamllsp", Docs: "https://github.com/ocaml/ocaml-lsp", Commands: map[string]string{"": "opam install ocaml-lsp-server"}},
}

func Metadata(language string) (executable, documentation string, ok bool) {
	m, ok := metadata[language]
	return m.Executable, m.Docs, ok
}

func DetectPackageManager(root string) string { return PackageManager(root) }

func PackageManager(root string) string {
	for name, manager := range map[string]string{"pnpm-lock.yaml": "pnpm", "yarn.lock": "yarn", "bun.lockb": "bun", "package-lock.json": "npm"} {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			return manager
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err == nil {
		var manifest struct {
			PackageManager string `json:"packageManager"`
		}
		if json.Unmarshal(data, &manifest) == nil {
			for _, manager := range []string{"pnpm", "yarn", "bun", "npm"} {
				if strings.HasPrefix(manifest.PackageManager, manager+"@") || manifest.PackageManager == manager {
					return manager
				}
			}
		}
	}
	return ""
}

func Guidance(language, root string, overridden bool) *InstallationGuidance {
	if overridden {
		return nil
	}
	m, ok := metadata[language]
	if !ok {
		return nil
	}
	g := &InstallationGuidance{Documentation: m.Docs}
	if command := m.Commands[PackageManager(root)]; command != "" {
		g.Command = command
	} else if command := m.Commands[""]; command != "" {
		g.Command = command
	}
	if g.Command == "" && g.Documentation == "" {
		return nil
	}
	return g
}

func ClassifyFailure(err error) DiagnosticCategory { return classifyFailure(err) }

func classifyFailure(err error) DiagnosticCategory {
	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return MissingExecutable
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) && (errors.Is(pathErr.Err, os.ErrNotExist) || errors.Is(pathErr.Err, os.ErrPermission)) {
		return MissingExecutable
	}
	return InitializationFailure
}

func newDiagnostic(language, executable, root string, err error, overridden bool) LSPDiagnostic {
	return LSPDiagnostic{Language: language, Executable: executable, Category: classifyFailure(err), Severity: SeverityError, Reason: fmt.Sprintf("%v", err), Guidance: Guidance(language, root, overridden), Overridden: overridden}
}
