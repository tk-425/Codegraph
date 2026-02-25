package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tk-425/Codegraph/internal/config"
	"github.com/tk-425/Codegraph/internal/db"
	"github.com/tk-425/Codegraph/internal/indexer"
)

var forceFlag bool

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build or rebuild the symbol database",
	Long: `Build the codegraph database by indexing all source files.

This command:
1. Scans for source files (respecting .cgignore)
2. Starts LSP servers for detected languages
3. Extracts symbols from all source files
4. Stores symbols in the database

Use --force to perform a full rebuild (delete and recreate database).`,
	RunE: runBuild,
}

func init() {
	buildCmd.Flags().BoolVar(&forceFlag, "force", false, "Force full rebuild (delete and recreate database)")
	rootCmd.AddCommand(buildCmd)
}

func runBuild(cmd *cobra.Command, args []string) error {
	printBanner(cmd.OutOrStdout())
	fmt.Println()

	if forceFlag {
		fmt.Printf("🔄 %s\n", Bold("Force rebuilding database..."))
	} else {
		fmt.Printf("🔨 %s\n", Bold("Building database..."))
	}

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

	// Scan for files
	cgignorePath := filepath.Join(codegraphDir, ".cgignore")
	scanner := indexer.NewScanner(cwd, cgignorePath)
	files, err := scanner.Scan()
	if err != nil {
		return fmt.Errorf("failed to scan files: %w", err)
	}

	languages := indexer.DetectedLanguages(files)
	if len(languages) == 0 {
		fmt.Printf("⚠️  %s\n", Warning("No supported source files found"))
		return nil
	}
	fmt.Printf("🔍 Found %s files in %s languages (%s)\n",
		Info(len(files)), Info(len(languages)), Keyword(strings.Join(languages, ", ")))

	// Open database
	dbPath := cfg.GetDatabasePath(cwd)
	dbManager, err := db.NewManager(dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer dbManager.Close()

	if err := dbManager.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}

	// Create indexer and run
	idx := indexer.NewIndexer(cfg, dbManager, cwd)
	defer idx.Close()

	ctx := context.Background()
	if err := idx.IndexProject(ctx, files, forceFlag); err != nil {
		return fmt.Errorf("indexing failed: %w", err)
	}

	return nil
}
