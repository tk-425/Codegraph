package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/tk-425/Codegraph/internal/registry"
)

var projectsCmd = &cobra.Command{
	Use:   "projects",
	Short: "List all tracked projects",
	RunE:  runProjects,
}

func init() {
	rootCmd.AddCommand(projectsCmd)
}

func runProjects(cmd *cobra.Command, args []string) error {
	regPath, err := registry.DefaultRegistryPath()
	if err != nil {
		return err
	}

	reg, err := registry.Load(regPath)
	if err != nil {
		return err
	}

	if len(reg.Projects) == 0 {
		fmt.Println("No projects found in registry")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tPATH\tSTATUS\tLAST SEEN")

	for path, proj := range reg.Projects {
		status := getProjectStatus(path)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			proj.Name,
			path,
			status,
			proj.LastSeen.Format(time.RFC822),
		)
	}
	w.Flush()
	return nil
}

func getProjectStatus(path string) string {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return "MISSING ⚠️"
	}
	if !info.IsDir() {
		return "INVALID (File)"
	}

	cgDir := filepath.Join(path, ".codegraph")
	if _, err := os.Stat(cgDir); os.IsNotExist(err) {
		return "Uninitialized ⚠️"
	}

	return "Active ✅"
}
