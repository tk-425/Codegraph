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
	rootCmd.AddCommand(typesCmd)
}

func runTypes(cmd *cobra.Command, args []string) error {
	symbol := args[0]
	fmt.Printf("🔗 Finding type hierarchy for: %s\n", symbol)
	
	if typesSupertypesFlag {
		fmt.Println("   Direction: supertypes")
	}
	if typesSubtypesFlag {
		fmt.Println("   Direction: subtypes")
	}
	if !typesSupertypesFlag && !typesSubtypesFlag {
		fmt.Println("   Direction: both")
	}
	if typesLangFlag != "" {
		fmt.Printf("   Languages: %s\n", typesLangFlag)
	}
	
	// TODO: Implement types logic
	fmt.Println("\n⚠️  Not yet implemented")
	return nil
}
