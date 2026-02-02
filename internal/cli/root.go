package cli

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "codegraph",
	Short: "Code indexing and call graph analysis tool",
	Long:  "CodeGraph indexes your codebase using LSP servers and provides fast symbol search, call graph analysis, and code navigation.",
}

func Execute() error {
	return rootCmd.Execute()
}
