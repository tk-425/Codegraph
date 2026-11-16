package lsp

import "github.com/tk-425/Codegraph/internal/config"

// BuildFlags returns the configured Go build flags for language: the gopls
// buildFlags setting, forwarded to the server so semantic extraction runs in
// the project's intended build context. Only go has a configured context.
func BuildFlags(cfg *config.Config, language string) []string {
	if language != "go" || cfg == nil {
		return nil
	}
	configured, ok := cfg.LSP["go"]
	if !ok {
		return nil
	}
	return append([]string(nil), configured.BuildFlags...)
}

// ServerSettings builds the gopls settings object for language, or nil when
// no settings apply. This map is the value gopls documents for
// initializationOptions and for each workspace/configuration item whose
// section is "gopls"; it is never gopls command-line arguments.
func ServerSettings(cfg *config.Config, language string) map[string]any {
	flags := BuildFlags(cfg, language)
	if len(flags) == 0 {
		return nil
	}
	return map[string]any{"buildFlags": flags}
}
