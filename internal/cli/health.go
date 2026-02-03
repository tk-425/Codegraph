package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/tk-425/Codegraph/internal/config"
	"github.com/tk-425/Codegraph/internal/db"
)

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Check the health of codegraph installation",
	Long: `Check the health of codegraph by verifying:
1. Database exists and is accessible
2. Symbol and call counts
3. Indexed languages`,
	RunE: runHealth,
}

func init() {
	rootCmd.AddCommand(healthCmd)
}

func runHealth(cmd *cobra.Command, args []string) error {
	fmt.Printf("🏥 %s\n\n", Bold("Checking codegraph health..."))

	// Get current directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Check if codegraph is initialized
	codegraphDir := filepath.Join(cwd, ".codegraph")
	if _, err := os.Stat(codegraphDir); os.IsNotExist(err) {
		fmt.Printf("❌ %s: Run 'codegraph init' first\n", Error("Not initialized"))
		return nil
	}
	fmt.Printf("✅ %s: .codegraph/ directory exists\n", Success("Initialized"))

	// Load config
	cfg, err := config.Load(cwd)
	if err != nil {
		fmt.Printf("❌ %s: %v\n", Error("Config error"), err)
		return nil
	}
	fmt.Printf("✅ %s: config.toml loaded\n", Success("Config"))

	// Check database
	dbPath := cfg.GetDatabasePath(cwd)
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		fmt.Printf("❌ %s: not found\n", Error("Database"))
		return nil
	}

	dbManager, err := db.NewManager(dbPath)
	if err != nil {
		fmt.Printf("❌ %s: %v\n", Error("Database error"), err)
		return nil
	}
	defer dbManager.Close()

	// Get stats
	stats, err := dbManager.GetStats()
	if err != nil {
		fmt.Printf("❌ %s: %v\n", Error("Stats error"), err)
		return nil
	}

	fmt.Printf("✅ %s: accessible\n", Success("Database"))
	fmt.Println()
	fmt.Printf("📊 %s\n", Bold("Statistics:"))
	fmt.Printf("   Symbols:   %s\n", Info(stats.SymbolCount))
	fmt.Printf("   Calls:     %s\n", Info(stats.CallCount))
	fmt.Printf("   Files:     %s\n", Info(stats.FileCount))

	if len(stats.Languages) > 0 {
		fmt.Printf("   Languages: %s\n", Keyword(stats.Languages))
	}

	// Check LSP servers
	fmt.Println()
	fmt.Printf("🔧 %s\n", Bold("LSP Servers:"))
	for lang, lspCfg := range cfg.LSP {
		// Check if command exists
		_, err := exec.LookPath(lspCfg.Command)
		if err != nil {
			fmt.Printf("   ❌ %s: %s not found\n", Warning(lang), Error(lspCfg.Command))
		} else {
			fmt.Printf("   ✅ %s: %s\n", Keyword(lang), Dim(lspCfg.Command))
		}
	}

	return nil
}
