package cli

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	codeGraphAgentNoteStart = "<!-- codegraph:start -->"
	codeGraphAgentNoteEnd   = "<!-- codegraph:end -->"
	codeGraphAgentNote      = codeGraphAgentNoteStart + `
## Optional CodeGraph usage

CodeGraph is an optional local code-navigation aid when ` + "`codegraph`" + ` is installed and initialized.

Use normal repo inspection first: ` + "`rg`" + `, file reads, test names, routes, imports, stack traces, or user-provided clues. Do not use CodeGraph for open-ended discovery.

Once you know a symbol, CodeGraph can help inspect impact:
- ` + "`codegraph signature <symbol>`" + `: inspect declaration/signature.
- ` + "`codegraph callers <symbol>`" + `: find call sites.
- ` + "`codegraph callees <symbol>`" + `: find downstream calls.
- ` + "`codegraph search <symbol>`" + `: confirm indexed symbol matches.

Treat CodeGraph output as navigation evidence, not source of truth. Read source files before making conclusions or edits.

If ` + "`.codegraph/`" + ` is missing, stale, or ` + "`codegraph`" + ` is unavailable, continue with normal repo inspection. Do not install, initialize, or rebuild CodeGraph without explicit approval.

` + codeGraphAgentNoteEnd + `
`
)

var agentInstructionFiles = []string{"CLAUDE.md", "GEMINI.md", "AGENTS.md"}

type agentNoteResult struct {
	Updated         []string
	Skipped         []string
	Warnings        []string
	ManualCopyBlock string
}

func applyAgentNote(projectRoot string) agentNoteResult {
	var result agentNoteResult

	for _, name := range agentInstructionFiles {
		path := filepath.Join(projectRoot, name)
		data, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				result.Warnings = append(result.Warnings, name+": "+err.Error())
			}
			continue
		}

		content := string(data)
		if strings.Contains(content, codeGraphAgentNoteStart) && strings.Contains(content, codeGraphAgentNoteEnd) {
			result.Skipped = append(result.Skipped, name)
			continue
		}

		updated := appendAgentNote(content)
		if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
			result.Warnings = append(result.Warnings, name+": "+err.Error())
			continue
		}
		result.Updated = append(result.Updated, name)
	}

	if len(result.Updated) == 0 && len(result.Skipped) == 0 && len(result.Warnings) == 0 {
		result.ManualCopyBlock = codeGraphAgentNote
	}

	return result
}

func appendAgentNote(content string) string {
	separator := "\n\n"
	if content == "" || strings.HasSuffix(content, "\n") {
		separator = "\n"
	}
	return content + separator + codeGraphAgentNote
}
