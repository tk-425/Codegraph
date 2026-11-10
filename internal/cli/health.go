package cli

import (
	"fmt"
	"os/exec"

	"github.com/spf13/cobra"
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
	supportJSON(healthCmd)
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

	// The same open sequence queries use, so health refuses what they refuse.
	_, cfg, dbManager, failure := openSequence()
	if failure.at(stepCwd) {
		return fmt.Errorf("failed to get current directory: %w", failure.err)
	}
	if failure.at(stepInitialized) {
		fmt.Printf("❌ %s: Run 'codegraph init' first\n", Error("Not initialized"))
		return nil
	}
	fmt.Printf("✅ %s: .codegraph/ directory exists\n", Success("Initialized"))

	if failure.at(stepConfig) {
		fmt.Printf("❌ %s: %v\n", Error("Config error"), failure.err)
		return nil
	}
	fmt.Printf("✅ %s: config.toml loaded\n", Success("Config"))

	switch {
	case failure.at(stepDatabaseFile):
		fmt.Printf("❌ %s: not found\n", Error("Database"))
		return nil
	case failure.at(stepDatabaseOpen):
		fmt.Printf("❌ %s: %v\n", Error("Database error"), failure.err)
		return nil
	case failure != nil:
		fmt.Printf("❌ %s: %v\n", Error("Database"), failure.err)
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

	_, cfg, dbManager, failure := openSequence()
	if failure.at(stepCwd) {
		_ = EmitJSON(out, "health", nil, records, []EnvelopeError{{Code: "cwd_failed", Message: failure.err.Error()}})
		return failure.err
	}

	if failure.at(stepInitialized) {
		records = append(records, healthRecord{Category: "initialized", Name: "initialized", OK: false, Detail: "codegraph not initialized. Run 'codegraph init' first"})
		return EmitJSON(out, "health", nil, records, nil)
	}
	records = append(records, healthRecord{Category: "initialized", Name: "initialized", OK: true, Detail: ".codegraph/ directory exists"})

	if failure.at(stepConfig) {
		records = append(records, healthRecord{Category: "config", Name: "config", OK: false, Detail: failure.err.Error()})
		return EmitJSON(out, "health", nil, records, nil)
	}
	records = append(records, healthRecord{Category: "config", Name: "config", OK: true, Detail: "config.toml loaded"})

	if failure.at(stepDatabaseFile) {
		records = append(records, healthRecord{Category: "database", Name: "database", OK: false, Detail: "not found"})
		return EmitJSON(out, "health", nil, records, nil)
	}
	if failure != nil {
		records = append(records, healthRecord{Category: "database", Name: "database", OK: false, Detail: failure.err.Error()})
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
