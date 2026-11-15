package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

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

type pruneCandidate struct {
	path     string
	lastSeen time.Time
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

	var toRemove []pruneCandidate
	for path, proj := range reg.Projects {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			toRemove = append(toRemove, pruneCandidate{path: path, lastSeen: proj.LastSeen})
		}
	}

	if len(toRemove) == 0 {
		fmt.Printf("✨ %s\n", Success("No missing projects found"))
		return nil
	}

	fmt.Printf("🗑️  Found %s missing projects:\n\n", Warning(len(toRemove)))
	for _, candidate := range toRemove {
		fmt.Printf("  %s %s\n", Error("✗"), Path(candidate.path))
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

	// Revalidate each approved candidate under the update lock: entries that a
	// concurrent registration refreshed, paths that exist again, and entries
	// another prune already removed all survive.
	removed := 0
	if err := registry.Update(cmd.Context(), regPath, func(latest *registry.Registry) error {
		for _, candidate := range toRemove {
			proj, ok := latest.Projects[candidate.path]
			if !ok {
				continue
			}
			if !proj.LastSeen.Equal(candidate.lastSeen) {
				continue
			}
			if _, err := os.Stat(candidate.path); !os.IsNotExist(err) {
				continue
			}
			latest.Remove(candidate.path)
			removed++
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to save registry: %w", err)
	}

	fmt.Printf("✅ %s\n", Success(fmt.Sprintf("Removed %d projects from registry", removed)))
	return nil
}
