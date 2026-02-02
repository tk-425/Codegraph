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
	calleesDepthFlag int
	calleesLangFlag  string
)

var calleesCmd = &cobra.Command{
	Use:   "callees <symbol>",
	Short: "Find all functions called by a given symbol",
	Long: `Find all functions that the specified symbol calls.

Examples:
  codegraph callees main
  codegraph callees handleRequest --depth=2
  codegraph callees process --lang=go`,
	Args: cobra.ExactArgs(1),
	RunE: runCallees,
}

func init() {
	calleesCmd.Flags().IntVar(&calleesDepthFlag, "depth", 1, "Depth of call chain to traverse")
	calleesCmd.Flags().StringVar(&calleesLangFlag, "lang", "", "Filter by language(s), comma-separated")
	rootCmd.AddCommand(calleesCmd)
}

func runCallees(cmd *cobra.Command, args []string) error {
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
	if calleesLangFlag != "" {
		languages = strings.Split(calleesLangFlag, ",")
	}

	// Find callees
	callees, err := dbManager.GetCallees(symbol, languages)
	if err != nil {
		return fmt.Errorf("failed to find callees: %w", err)
	}

	if len(callees) == 0 {
		fmt.Printf("📤 No callees found for: %s\n", Warning(symbol))
		return nil
	}

	fmt.Printf("📤 Callees of %s (%s found):\n\n", Symbol(symbol), Info(len(callees)))
	for _, c := range callees {
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
