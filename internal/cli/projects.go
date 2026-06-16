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

type projectRecord struct {
	Name          string    `json:"name"`
	Path          string    `json:"path"`
	Status        string    `json:"status"`
	LastSeen      time.Time `json:"last_seen"`
	InitializedAt time.Time `json:"initialized_at"`
}

func runProjects(cmd *cobra.Command, args []string) error {
	if jsonOutputFlag {
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		return runProjectsJSON(cmd)
	}

	regPath, err := registry.DefaultRegistryPath()
	if err != nil {
		return err
	}

	reg, err := registry.Load(regPath)
	if err != nil {
		return err
	}

	if len(reg.Projects) == 0 {
		fmt.Printf("📁 %s\n", Warning("No projects found in registry"))
		return nil
	}

	fmt.Printf("📁 Projects (%s found):\n\n", Info(len(reg.Projects)))

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, Bold("NAME")+"\t"+Bold("PATH")+"\t"+Bold("STATUS")+"\t"+Bold("LAST SEEN"))

	for path, proj := range reg.Projects {
		status := getProjectStatus(path)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			Symbol(proj.Name),
			Path(path),
			status,
			Dim(proj.LastSeen.Format(time.RFC822)),
		)
	}
	w.Flush()
	return nil
}

func runProjectsJSON(cmd *cobra.Command) error {
	out := cmd.OutOrStdout()
	emitErr := func(code string, err error) error {
		_ = EmitJSON(out, "projects", nil, []projectRecord{}, []EnvelopeError{{Code: code, Message: err.Error()}})
		return err
	}

	regPath, err := registry.DefaultRegistryPath()
	if err != nil {
		return emitErr("registry_path_failed", err)
	}
	reg, err := registry.Load(regPath)
	if err != nil {
		return emitErr("registry_load_failed", err)
	}

	records := make([]projectRecord, 0, len(reg.Projects))
	for path, proj := range reg.Projects {
		records = append(records, projectRecord{
			Name:          proj.Name,
			Path:          path,
			Status:        getProjectStatusCode(path),
			LastSeen:      proj.LastSeen,
			InitializedAt: proj.InitializedAt,
		})
	}

	return EmitJSON(out, "projects", nil, records, nil)
}

func getProjectStatusCode(path string) string {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return "missing"
	}
	if !info.IsDir() {
		return "invalid"
	}
	if _, err := os.Stat(filepath.Join(path, ".codegraph")); os.IsNotExist(err) {
		return "uninitialized"
	}
	return "active"
}

func getProjectStatus(path string) string {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return "❌ " + Error("Missing")
	}
	if !info.IsDir() {
		return "⚠️ " + Warning("Invalid")
	}

	cgDir := filepath.Join(path, ".codegraph")
	if _, err := os.Stat(cgDir); os.IsNotExist(err) {
		return "⚠️ " + Warning("Uninitialized")
	}

	return "✅ " + Success("Active")
}
