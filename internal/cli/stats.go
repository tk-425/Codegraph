package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/tk-425/Codegraph/internal/config"
	"github.com/tk-425/Codegraph/internal/db"
)

var statsCompact bool

type statsLangRecord struct {
	Language string  `json:"language"`
	Count    int     `json:"count"`
	Percent  float64 `json:"percent"`
}

type statsRecord struct {
	TotalSymbols  int               `json:"total_symbols"`
	Functions     int               `json:"functions"`
	Methods       int               `json:"methods"`
	Classes       int               `json:"classes"`
	Interfaces    int               `json:"interfaces"`
	Structs       int               `json:"structs"`
	Types         int               `json:"types"`
	Enums         int               `json:"enums"`
	Variables     int               `json:"variables"`
	Constants     int               `json:"constants"`
	Modules       int               `json:"modules"`
	CallEdges     int               `json:"call_edges"`
	Languages     []statsLangRecord `json:"languages"`
	LastBuildTime *time.Time        `json:"last_build_time"`
	FilesIndexed  int               `json:"files_indexed"`
	DatabasePath  string            `json:"database_path"`
	DatabaseSize  int64             `json:"database_size"`
}

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
	statsCmd.Flags().BoolVar(&statsCompact, "compact", false, "Compact output format")
}

func runStats(cmd *cobra.Command, args []string) error {
	if jsonOutputFlag {
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		return runStatsJSON(cmd)
	}

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
	if statsCompact {
		return outputStatsCompact(stats)
	}

	// Default formatted output
	printStats(stats, cwd)
	return nil
}

func runStatsJSON(cmd *cobra.Command) error {
	out := cmd.OutOrStdout()
	emitErr := func(code string, err error) error {
		_ = EmitJSON(out, "stats", nil, []statsRecord{}, []EnvelopeError{{Code: code, Message: err.Error()}})
		return err
	}

	_, _, dbManager, code, err := openProject(true)
	if err != nil {
		return emitErr(code, err)
	}
	defer dbManager.Close()

	stats, err := dbManager.GetDetailedStats()
	if err != nil {
		return emitErr("stats_failed", fmt.Errorf("failed to get stats: %w", err))
	}

	langs := make([]statsLangRecord, 0, len(stats.Languages))
	for _, l := range stats.Languages {
		langs = append(langs, statsLangRecord{Language: l.Language, Count: l.Count, Percent: l.Percent})
	}

	rec := statsRecord{
		TotalSymbols:  stats.TotalSymbols,
		Functions:     stats.Functions,
		Methods:       stats.Methods,
		Classes:       stats.Classes,
		Interfaces:    stats.Interfaces,
		Structs:       stats.Structs,
		Types:         stats.Types,
		Enums:         stats.Enums,
		Variables:     stats.Variables,
		Constants:     stats.Constants,
		Modules:       stats.Modules,
		CallEdges:     stats.CallEdges,
		Languages:     langs,
		LastBuildTime: stats.LastBuildTime,
		FilesIndexed:  stats.FilesIndexed,
		DatabasePath:  stats.DatabasePath,
		DatabaseSize:  stats.DatabaseSize,
	}

	return EmitJSON(out, "stats", nil, []statsRecord{rec}, nil)
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
