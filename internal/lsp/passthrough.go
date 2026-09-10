package lsp

import "os"

// StderrPassthroughEnv enables stderr passthrough for callers that do not
// construct the command line, such as CI pipelines and agent harnesses.
const StderrPassthroughEnv = "CODEGRAPH_LSP_STDERR"

// StderrPassthrough is set by the persistent --lsp-stderr root flag. When true,
// raw language-server standard error is forwarded instead of discarded.
var StderrPassthrough bool

// StderrPassthroughEnabled reports whether stderr passthrough is active, either
// from the --lsp-stderr flag or a non-empty CODEGRAPH_LSP_STDERR environment variable.
func StderrPassthroughEnabled() bool {
	return StderrPassthrough || os.Getenv(StderrPassthroughEnv) != ""
}
