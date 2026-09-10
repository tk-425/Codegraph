package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/tk-425/Codegraph/internal/lsp"
)

var rootCmd = &cobra.Command{
	Use:   "codegraph",
	Short: "Code indexing and call graph analysis tool",
	Long:  "CodeGraph indexes your codebase using LSP servers and provides fast symbol search, call graph analysis, and code navigation.",
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutputFlag, "json", false, "Emit machine-readable JSON output (read-only query commands only)")
	rootCmd.PersistentFlags().BoolVar(&lsp.StderrPassthrough, "lsp-stderr", false, "Forward raw language-server standard error instead of discarding it")

	defaultHelp := rootCmd.HelpFunc()
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		out := cmd.OutOrStdout()
		if !jsonOutputFlag {
			printBanner(out)
			fmt.Fprintln(out)
		}
		defaultHelp(cmd, args)
	})
}
