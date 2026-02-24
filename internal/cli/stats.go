package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/tk-425/Codegraph/internal/config"
	"github.com/tk-425/Codegraph/internal/db"
)

var (
	statsJSON    bool
	statsCompact bool
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show codegraph statistics for current project",
	Long: `Display comprehensive statistics about the indexed codebase.

Shows symbol counts by kind, call graph edges, language breakdown,
last build time, and database information.`,
	RunE: runStats,
}

func init() {
	rootCmd.AddCommand(statsCmd)
	statsCmd.Flags().BoolVar(&statsJSON, "json", false, "Output as JSON")
	statsCmd.Flags().BoolVar(&statsCompact, "compact", false, "Compact output format")
}

func runStats(cmd *cobra.Command, args []string) error {
	// Get current directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Check if codegraph is initialized
	codegraphDir := filepath.Join(cwd, ".codegraph")
	if _, err := os.Stat(codegraphDir); os.IsNotExist(err) {
		return fmt.Errorf("not initialized. Run 'codegraph init' first")
	}

	// Load config
	cfg, err := config.Load(cwd)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Get database path
	dbPath := cfg.GetDatabasePath(cwd)
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return fmt.Errorf("database not found. Run 'codegraph build' first")
	}

	// Open database
	dbManager, err := db.NewManager(dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer dbManager.Close()

	// Get detailed stats
	stats, err := dbManager.GetDetailedStats()
	if err != nil {
		return fmt.Errorf("failed to get stats: %w", err)
	}

	// Output based on flags
	if statsJSON {
		return outputStatsJSON(stats)
	}
	if statsCompact {
		return outputStatsCompact(stats)
	}

	// Default formatted output
	printStats(stats, cwd)
	return nil
}

func printStats(stats *db.DetailedStats, projectPath string) {
	// Header
	fmt.Printf("CodeGraph Status for: %s\n\n", Path(projectPath))

	// Index Statistics
	fmt.Printf("📊 %s\n", Bold("Index Statistics"))
	fmt.Printf("   Symbols:      %s\n", Info(formatNumber(stats.TotalSymbols)))
	fmt.Printf("   Functions:    %s\n", Info(formatNumber(stats.Functions)))
	fmt.Printf("   Methods:      %s\n", Info(formatNumber(stats.Methods)))
	fmt.Printf("   Classes:      %s\n", Info(formatNumber(stats.Classes)))
	fmt.Printf("   Interfaces:   %s\n", Info(formatNumber(stats.Interfaces)))
	fmt.Printf("   Structs:      %s\n", Info(formatNumber(stats.Structs)))
	fmt.Printf("   Types:        %s\n", Info(formatNumber(stats.Types)))
	fmt.Printf("   Variables:    %s\n", Info(formatNumber(stats.Variables)))
	fmt.Printf("   Constants:    %s\n", Info(formatNumber(stats.Constants)))
	fmt.Printf("   Call edges:   %s\n", Info(formatNumber(stats.CallEdges)))
	fmt.Println()

	// Languages
	if len(stats.Languages) > 0 {
		fmt.Printf("🗂️  %s\n", Bold("Languages"))
		for _, lang := range stats.Languages {
			fmt.Printf("   %-12s %s symbols (%s)\n",
				Keyword(lang.Language)+":",
				Info(formatNumber(lang.Count)),
				Info(fmt.Sprintf("%.1f%%", lang.Percent)))
		}
		fmt.Println()
	}

	// Last Build
	fmt.Printf("📅 %s\n", Bold("Last Build"))
	if stats.LastBuildTime != nil {
		fmt.Printf("   Time:    %s\n", Info(formatTime(stats.LastBuildTime)))
		fmt.Printf("   Files:   %s tracked\n", Info(formatNumber(stats.FilesIndexed)))
	} else {
		fmt.Printf("   %s\n", Dim("No build data available"))
	}
	fmt.Println()

	// Database
	fmt.Printf("💾 %s\n", Bold("Database"))
	fmt.Printf("   Path:    %s\n", Path(stats.DatabasePath))
	fmt.Printf("   Size:    %s\n", Info(formatBytes(stats.DatabaseSize)))
}

func outputStatsJSON(stats *db.DetailedStats) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(stats)
}

func outputStatsCompact(stats *db.DetailedStats) error {
	fmt.Printf("symbols:%d functions:%d methods:%d classes:%d edges:%d\n",
		stats.TotalSymbols, stats.Functions, stats.Methods,
		stats.Classes, stats.CallEdges)
	return nil
}

func formatNumber(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1000000 {
		return fmt.Sprintf("%d,%03d", n/1000, n%1000)
	}
	return fmt.Sprintf("%d,%03d,%03d", n/1000000, (n/1000)%1000, n%1000)
}

func formatTime(t *time.Time) string {
	if t == nil {
		return "Never"
	}
	return t.Format("2006-01-02 15:04:05")
}

func formatBytes(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	}
	if bytes < 1024*1024*1024 {
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
	}
	return fmt.Sprintf("%.1f GB", float64(bytes)/(1024*1024*1024))
}
