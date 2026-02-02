package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tk-425/Codegraph/internal/registry"
)

var pruneForceFlag bool

var pruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Remove missing projects from registry",
	RunE:  runPrune,
}

func init() {
	pruneCmd.Flags().BoolVarP(&pruneForceFlag, "force", "f", false, "Force remove without prompt")
	rootCmd.AddCommand(pruneCmd)
}

func runPrune(cmd *cobra.Command, args []string) error {
	regPath, err := registry.DefaultRegistryPath()
	if err != nil {
		return err
	}

	reg, err := registry.Load(regPath)
	if err != nil {
		return err
	}

	var toRemove []string
	for path := range reg.Projects {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			toRemove = append(toRemove, path)
		}
	}

	if len(toRemove) == 0 {
		fmt.Println("✨ No missing projects found")
		return nil
	}

	fmt.Printf("Found %d missing projects:\n", len(toRemove))
	for _, p := range toRemove {
		fmt.Printf("  - %s\n", p)
	}

	if !pruneForceFlag {
		fmt.Print("\nRemove these from registry? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.ToLower(strings.TrimSpace(response))

		if response != "y" && response != "yes" {
			fmt.Println("Aborted")
			return nil
		}
	}

	for _, p := range toRemove {
		reg.Remove(p)
	}

	if err := reg.Save(regPath); err != nil {
		return fmt.Errorf("failed to save registry: %w", err)
	}

	fmt.Printf("✅ Removed %d projects from registry\n", len(toRemove))
	return nil
}
