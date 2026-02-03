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
		fmt.Printf("✨ %s\n", Success("No missing projects found"))
		return nil
	}

	fmt.Printf("🗑️  Found %s missing projects:\n\n", Warning(len(toRemove)))
	for _, p := range toRemove {
		fmt.Printf("  %s %s\n", Error("✗"), Path(p))
	}

	if !pruneForceFlag {
		fmt.Printf("\n%s [y/N] ", Bold("Remove these from registry?"))
		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.ToLower(strings.TrimSpace(response))

		if response != "y" && response != "yes" {
			fmt.Printf("%s\n", Warning("Aborted"))
			return nil
		}
	}

	for _, p := range toRemove {
		reg.Remove(p)
	}

	if err := reg.Save(regPath); err != nil {
		return fmt.Errorf("failed to save registry: %w", err)
	}

	fmt.Printf("✅ %s\n", Success(fmt.Sprintf("Removed %d projects from registry", len(toRemove))))
	return nil
}
