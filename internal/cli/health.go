package cli

import (
	"fmt"
	"os/exec"

	"github.com/spf13/cobra"
	"github.com/tk-425/Codegraph/internal/config"
	"github.com/tk-425/Codegraph/internal/lsp"
)

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Check the current project's index and LSP availability",
	Long: `Check the current project's configuration, read-only database access,
indexed counts and snapshot quality, and effective LSP commands and arguments.
LSP checks establish executable availability only: servers are not started,
and initialization and capabilities remain unchecked.`,
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
	projectRoot, cfg, dbManager, failure := openSequence(commandContext(cmd))
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

	metadata, err := queryMetadata(dbManager)
	if err != nil {
		fmt.Printf("❌ Snapshot: %v\n", err)
	} else if metadata.Snapshot == nil {
		fmt.Println("❌ Snapshot: unavailable; quality unknown; freshness unchecked")
	} else {
		fmt.Printf("Snapshot: %s (%s; freshness unchecked)\n", metadata.Snapshot.ID, metadata.Snapshot.Quality)
	}

	// Check LSP servers
	fmt.Println()
	fmt.Printf("🔧 %s\n", Bold("LSP Servers:"))
	for _, record := range lspHealthRecords(cfg, projectRoot) {
		if record.OK {
			fmt.Printf("   ✅ %s: %s\n", Keyword(record.Name), Dim(record.Detail))
		} else {
			fmt.Printf("   ❌ %s: %s\n", Warning(record.Name), Error(record.Detail))
		}
	}

	return nil
}

func runHealthJSON(cmd *cobra.Command) error {
	out := cmd.OutOrStdout()
	records := []healthRecord{}

	projectRoot, cfg, dbManager, failure := openSequence(commandContext(cmd))
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

	metadata, metaErr := queryMetadata(dbManager)
	quality := healthRecord{Category: "snapshot", Name: "quality", OK: false, Detail: "unavailable; quality unknown; freshness unchecked"}
	if metaErr != nil {
		quality.Detail = metaErr.Error()
	} else if metadata.Snapshot != nil {
		quality.OK = metadata.Snapshot.Quality == "complete"
		quality.Detail = fmt.Sprintf("%s; %s; freshness unchecked", metadata.Snapshot.ID, metadata.Snapshot.Quality)
	}
	records = append(records, quality)
	records = append(records, lspHealthRecords(cfg, projectRoot)...)

	return EmitJSON(out, "health", nil, records, nil, metadata)
}

// Availability is a lookup only; neither selection nor health starts a server.
func lspHealthRecords(cfg *config.Config, projectRoot string) []healthRecord {
	records := make([]healthRecord, 0, len(cfg.LSP))
	const unchecked = "; initialization unchecked; capabilities unchecked"
	for language := range cfg.LSP {
		record := healthRecord{Category: "lsp", Name: language}
		server, err := lsp.ResolveServer(cfg, projectRoot, language)
		if err != nil {
			record.Detail = "configuration: " + err.Error() + unchecked
		} else {
			detail := fmt.Sprintf("command=%q; args=%q; ", server.Command, server.Args)
			if _, err := exec.LookPath(server.Command); err != nil {
				record.Detail = detail + "executable unavailable: " + err.Error() + unchecked
			} else {
				record.OK = true
				record.Detail = detail + "executable available" + unchecked
			}
		}
		records = append(records, record)
	}
	return records
}
