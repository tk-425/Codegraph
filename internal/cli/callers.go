package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tk-425/Codegraph/internal/config"
	"github.com/tk-425/Codegraph/internal/db"
)

var (
	callersDepthFlag int
	callersLangFlag  string
)

var callersCmd = &cobra.Command{
	Use:   "callers <symbol>",
	Short: "Find all functions that call a given symbol",
	Long: `Find all functions that call the specified symbol.

Examples:
  codegraph callers parseConfig
  codegraph callers handleRequest --depth=2
  codegraph callers parse --lang=go,python`,
	Args: cobra.ExactArgs(1),
	RunE: runCallers,
}

func init() {
	callersCmd.Flags().IntVar(&callersDepthFlag, "depth", 1, "Depth of call chain to traverse")
	callersCmd.Flags().StringVar(&callersLangFlag, "lang", "", "Filter by language(s), comma-separated")
	rootCmd.AddCommand(callersCmd)
}

func runCallers(cmd *cobra.Command, args []string) error {
	symbol := args[0]

	// Get current directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Check if codegraph is initialized
	codegraphDir := filepath.Join(cwd, ".codegraph")
	if _, err := os.Stat(codegraphDir); os.IsNotExist(err) {
		return fmt.Errorf("codegraph not initialized. Run 'codegraph init' first")
	}

	// Load config
	cfg, err := config.Load(cwd)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Open database
	dbPath := cfg.GetDatabasePath(cwd)
	dbManager, err := db.NewManager(dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer dbManager.Close()

	// Parse languages filter
	var languages []string
	if callersLangFlag != "" {
		languages = strings.Split(callersLangFlag, ",")
	}

	// Find callers
	callers, err := dbManager.GetCallers(symbol, languages)
	if err != nil {
		return fmt.Errorf("failed to find callers: %w", err)
	}

	if len(callers) == 0 {
		fmt.Printf("📞 No callers found for: %s\n", Warning(symbol))
		return nil
	}

	fmt.Printf("📞 Callers of %s (%s found):\n\n", Symbol(symbol), Info(len(callers)))
	for _, c := range callers {
		relPath, _ := filepath.Rel(cwd, c.File)
		fmt.Printf("  %s [%s]\n", Symbol(c.Name), Keyword(c.Kind))
		fmt.Printf("    %s\n", Path(fmt.Sprintf("%s:%d", relPath, c.Line)))
		if c.Signature != "" {
			fmt.Printf("    %s\n", colorizeSignature(c.Signature))
		}
		fmt.Println()
	}

	return nil
}
