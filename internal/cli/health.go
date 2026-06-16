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

type healthRecord struct {
	Category string `json:"category"`
	Name     string `json:"name"`
	OK       bool   `json:"ok"`
	Detail   string `json:"detail"`
}

func runHealth(cmd *cobra.Command, args []string) error {
	if jsonOutputFlag {
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		return runHealthJSON(cmd)
	}

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

func runHealthJSON(cmd *cobra.Command) error {
	out := cmd.OutOrStdout()
	records := []healthRecord{}

	cwd, err := os.Getwd()
	if err != nil {
		_ = EmitJSON(out, "health", nil, records, []EnvelopeError{{Code: "cwd_failed", Message: err.Error()}})
		return err
	}

	codegraphDir := filepath.Join(cwd, ".codegraph")
	if _, err := os.Stat(codegraphDir); os.IsNotExist(err) {
		records = append(records, healthRecord{Category: "initialized", Name: "initialized", OK: false, Detail: "codegraph not initialized. Run 'codegraph init' first"})
		return EmitJSON(out, "health", nil, records, nil)
	}
	records = append(records, healthRecord{Category: "initialized", Name: "initialized", OK: true, Detail: ".codegraph/ directory exists"})

	cfg, err := config.Load(cwd)
	if err != nil {
		records = append(records, healthRecord{Category: "config", Name: "config", OK: false, Detail: err.Error()})
		return EmitJSON(out, "health", nil, records, nil)
	}
	records = append(records, healthRecord{Category: "config", Name: "config", OK: true, Detail: "config.toml loaded"})

	dbPath := cfg.GetDatabasePath(cwd)
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		records = append(records, healthRecord{Category: "database", Name: "database", OK: false, Detail: "not found"})
		return EmitJSON(out, "health", nil, records, nil)
	}

	dbManager, err := db.NewManager(dbPath)
	if err != nil {
		records = append(records, healthRecord{Category: "database", Name: "database", OK: false, Detail: err.Error()})
		return EmitJSON(out, "health", nil, records, nil)
	}
	defer dbManager.Close()

	stats, err := dbManager.GetStats()
	if err != nil {
		records = append(records, healthRecord{Category: "stats", Name: "stats", OK: false, Detail: err.Error()})
		return EmitJSON(out, "health", nil, records, nil)
	}
	records = append(records, healthRecord{Category: "database", Name: "database", OK: true, Detail: "accessible"})
	records = append(records, healthRecord{Category: "stats", Name: "symbols", OK: true, Detail: fmt.Sprintf("%d", stats.SymbolCount)})
	records = append(records, healthRecord{Category: "stats", Name: "calls", OK: true, Detail: fmt.Sprintf("%d", stats.CallCount)})
	records = append(records, healthRecord{Category: "stats", Name: "files", OK: true, Detail: fmt.Sprintf("%d", stats.FileCount)})

	for lang, lspCfg := range cfg.LSP {
		if _, err := exec.LookPath(lspCfg.Command); err != nil {
			records = append(records, healthRecord{Category: "lsp", Name: lang, OK: false, Detail: lspCfg.Command + " not found"})
		} else {
			records = append(records, healthRecord{Category: "lsp", Name: lang, OK: true, Detail: lspCfg.Command})
		}
	}

	return EmitJSON(out, "health", nil, records, nil)
}
