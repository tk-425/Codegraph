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

var signatureLangFlag string

var signatureCmd = &cobra.Command{
	Use:   "signature <symbol>",
	Short: "Get the signature of a function or method",
	Long: `Get the full signature of a function or method including parameters and return type.

Examples:
  codegraph signature parseConfig
  codegraph signature handleRequest --lang=go`,
	Args: cobra.ExactArgs(1),
	RunE: runSignature,
}

func init() {
	signatureCmd.Flags().StringVar(&signatureLangFlag, "lang", "", "Filter by language(s), comma-separated")
	rootCmd.AddCommand(signatureCmd)
}

func runSignature(cmd *cobra.Command, args []string) error {
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
	if signatureLangFlag != "" {
		languages = strings.Split(signatureLangFlag, ",")
	}

	// Find symbols in database
	symbols, err := dbManager.GetSymbolByName(symbol, languages)
	if err != nil {
		return fmt.Errorf("failed to find symbol: %w", err)
	}

	// Filter to only functions/methods
	var filtered []db.Symbol
	for _, sym := range symbols {
		if sym.Kind == "function" || sym.Kind == "method" {
			filtered = append(filtered, sym)
		}
	}

	if len(filtered) == 0 {
		fmt.Printf("📝 No function/method named '%s' found\n", Warning(symbol))
		return nil
	}

	fmt.Printf("📝 Signature for '%s' (%s found):\n\n", Symbol(symbol), Info(len(filtered)))

	for _, sym := range filtered {
		relPath, _ := filepath.Rel(cwd, sym.File)

		fmt.Printf("  %s [%s]\n", Symbol(sym.Name), Keyword(sym.Kind))
		fmt.Printf("    %s\n", Path(fmt.Sprintf("%s:%d", relPath, sym.Line)))

		// Show signature and source line
		sourceLine := getSourceLine(sym.File, sym.Line)
		if sym.Signature != "" && strings.TrimSpace(sym.Signature) != "" {
			// Show: signature → source
			sig := strings.TrimSpace(sym.Signature)
			if sourceLine != "" {
				fmt.Printf("    %s → %s\n", Type(sig), Dim(sourceLine))
			} else {
				fmt.Printf("    %s\n", Type(sig))
			}
		} else if sourceLine != "" {
			fmt.Printf("    %s\n", Dim(sourceLine))
		}
		fmt.Println()
	}

	return nil
}

// colorizeSignature adds colors to a function signature
func colorizeSignature(sig string) string {
	// Simple colorization: func keyword in cyan
	if len(sig) >= 4 && sig[:4] == "func" {
		return Keyword("func") + sig[4:]
	}
	return sig
}
