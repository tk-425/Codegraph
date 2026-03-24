package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tk-425/Codegraph/internal/config"
	"github.com/tk-425/Codegraph/internal/ignore"
	"github.com/tk-425/Codegraph/internal/registry"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize codegraph in the current project",
	Long: `Initialize codegraph by:
1. Creating .codegraph/ directory
2. Creating config.toml with LSP configurations
3. Creating .cgignore seeded from .gitignore
4. Adding .codegraph/ to .gitignore

After initialization, edit .codegraph/.cgignore to customize what gets indexed,
then run 'codegraph build' to index your project.`,
	RunE: runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	fmt.Printf("📁 %s\n", Bold("Initializing codegraph..."))

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
	fmt.Printf("📁 Created %s\n", Path(".codegraph/config.toml"))

	// 3. Create .cgignore
	if err := ignore.CreateDefaultCGIgnore(codegraphDir, cwd); err != nil {
		return fmt.Errorf("failed to create .cgignore: %w", err)
	}
	fmt.Printf("📁 Created %s (seeded from %s when available)\n", Path(".codegraph/.cgignore"), Dim("\".gitignore\""))

	// 4. Update .gitignore
	if err := updateGitignore(cwd); err != nil {
		fmt.Printf("⚠️  %s: %v\n", Warning("Could not update .gitignore"), err)
	} else {
		fmt.Printf("📝 Added %s to .gitignore\n", Dim("\".codegraph/\""))
	}

	// 5. Register project
	if err := registerProject(cwd); err != nil {
		fmt.Printf("⚠️  %s: %v\n", Warning("Failed to register project"), err)
	} else {
		fmt.Printf("📋 %s\n", Success("Registered project in global registry"))
	}

	fmt.Printf("✅ %s\n", Success("Done. Edit .codegraph/.cgignore to customize what gets indexed, then run 'codegraph build'."))

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
