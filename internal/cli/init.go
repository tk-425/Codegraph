package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tk-425/Codegraph/internal/config"
	"github.com/tk-425/Codegraph/internal/db"
	"github.com/tk-425/Codegraph/internal/ignore"
	"github.com/tk-425/Codegraph/internal/indexer"
	"github.com/tk-425/Codegraph/internal/registry"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize codegraph in the current project",
	Long: `Initialize codegraph by:
1. Creating .codegraph/ directory
2. Creating config.toml with LSP configurations
3. Creating .cgignore for excluding files
4. Adding .codegraph/ to .gitignore
5. Auto-detecting languages in the project
6. Running initial indexing`,
	RunE: runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	fmt.Println("📁 Initializing codegraph...")

	// Get current directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// 1. Create .codegraph directory
	codegraphDir := filepath.Join(cwd, ".codegraph")
	graphsDir := filepath.Join(codegraphDir, "graphs")
	if err := os.MkdirAll(graphsDir, 0755); err != nil {
		return fmt.Errorf("failed to create .codegraph directory: %w", err)
	}

	// 2. Create config.toml
	cfg := config.DefaultConfig()
	if err := config.Save(cwd, cfg); err != nil {
		return fmt.Errorf("failed to create config: %w", err)
	}
	fmt.Println("📁 Created .codegraph/config.toml")

	// 3. Create .cgignore
	cgignorePath := filepath.Join(codegraphDir, ".cgignore")
	if err := ignore.CreateDefaultCGIgnore(codegraphDir); err != nil {
		return fmt.Errorf("failed to create .cgignore: %w", err)
	}
	fmt.Println("📁 Created .codegraph/.cgignore")

	// 4. Update .gitignore
	if err := updateGitignore(cwd); err != nil {
		fmt.Printf("⚠️  Could not update .gitignore: %v\n", err)
	} else {
		fmt.Println("📝 Added \".codegraph/\" to .gitignore")
	}

	// 5. Detect languages
	scanner := indexer.NewScanner(cwd, cgignorePath)
	files, err := scanner.Scan()
	if err != nil {
		return fmt.Errorf("failed to scan files: %w", err)
	}

	languages := indexer.DetectedLanguages(files)
	if len(languages) == 0 {
		fmt.Println("⚠️  No supported source files found")
		return nil
	}
	fmt.Printf("🔍 Detected languages: %s\n", strings.Join(languages, ", "))

	// 6. Run indexing
	fmt.Println("🚀 Starting indexing...")

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
	if err := idx.IndexProject(ctx, files, true); err != nil {
		return fmt.Errorf("indexing failed: %w", err)
	}

	// 7. Register project
	if err := registerProject(cwd); err != nil {
		fmt.Printf("⚠️  Failed to register project: %v\n", err)
	} else {
		fmt.Println("📋 Registered project in global registry")
	}

	return nil
}

func registerProject(cwd string) error {
	regPath, err := registry.DefaultRegistryPath()
	if err != nil {
		return err
	}

	reg, err := registry.Load(regPath)
	if err != nil {
		return err
	}

	reg.Add(cwd, filepath.Base(cwd))
	return reg.Save(regPath)
}

// updateGitignore adds .codegraph/ to .gitignore if not already present
func updateGitignore(projectRoot string) error {
	gitignorePath := filepath.Join(projectRoot, ".gitignore")
	entry := ".codegraph/"

	// Check if .gitignore exists and already has entry
	if data, err := os.ReadFile(gitignorePath); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			if strings.TrimSpace(scanner.Text()) == entry {
				return nil // Already present
			}
		}
	}

	// Append entry
	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	// Add newline if file doesn't end with one
	if info, _ := f.Stat(); info.Size() > 0 {
		f.WriteString("\n")
	}
	_, err = f.WriteString(entry + "\n")
	return err
}
