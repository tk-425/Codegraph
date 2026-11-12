package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	typesSupertypesFlag bool
	typesSubtypesFlag   bool
	typesLangFlag       string
)

var typesCmd = &cobra.Command{
	Use:   "types <symbol>",
	Short: "Find type hierarchy (superclasses/subclasses)",
	Long: `Find the type hierarchy for a class or interface.

Examples:
  codegraph types ConfigManager --supertypes
  codegraph types BaseHandler --subtypes
  codegraph types Service --lang=go,java`,
	Args: cobra.ExactArgs(1),
	RunE: runTypes,
}

func init() {
	typesCmd.Flags().BoolVar(&typesSupertypesFlag, "supertypes", false, "Show parent types (superclasses, interfaces)")
	typesCmd.Flags().BoolVar(&typesSubtypesFlag, "subtypes", false, "Show child types (subclasses, implementors)")
	typesCmd.Flags().StringVar(&typesLangFlag, "lang", "", "Filter by language(s), comma-separated")
	supportJSON(typesCmd)
	rootCmd.AddCommand(typesCmd)
}

type typeRecord struct {
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	File         string `json:"file"`
	Line         int    `json:"line"`
	Relationship string `json:"relationship"`
}

func runTypes(cmd *cobra.Command, args []string) error {
	symbol := args[0]
	err := withCode("not_implemented", fmt.Errorf("types command not yet implemented"))
	if jsonOutputFlag {
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		_ = EmitJSON(cmd.OutOrStdout(), "types", &symbol, []typeRecord{},
			[]EnvelopeError{{Code: "not_implemented", Message: err.Error()}})
	}

	return err
}
