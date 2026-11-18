package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tk-425/Codegraph/internal/registry"
)

func runProjectsDeduplicate(cmd *cobra.Command) error {
	out := cmd.OutOrStdout()
	fail := func(code string, err error) error {
		if jsonOutputFlag {
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true
			_ = EmitJSON(out, "projects", nil, []registry.DuplicateGroup{}, []EnvelopeError{{Code: code, Message: err.Error()}})
		}
		return withCode(code, err)
	}
	if !projectsDeduplicateFlag {
		return fail("invalid_arguments", fmt.Errorf("--apply requires --deduplicate"))
	}
	path, err := registry.DefaultRegistryPath()
	if err != nil {
		return fail("registry_path_failed", err)
	}
	groups, err := registry.Deduplicate(cmd.Context(), path, projectsApplyFlag)
	if err != nil {
		code := "registry_load_failed"
		if projectsApplyFlag {
			code = "registry_repair_failed"
		}
		return fail(code, err)
	}
	if jsonOutputFlag {
		return EmitJSON(out, "projects", nil, groups, nil)
	}
	if len(groups) == 0 {
		_, err := fmt.Fprintln(out, "No duplicate projects found; registry unchanged.")
		return err
	}
	action := "Would merge"
	if projectsApplyFlag {
		action = "Merged"
	}
	for _, group := range groups {
		if _, err := fmt.Fprintf(out, "%s %d records into %s\n", action, len(group.Paths), group.Project.Path); err != nil {
			return err
		}
		for _, path := range group.Paths {
			if _, err := fmt.Fprintf(out, "  %s\n", path); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(out, "  name: %s; initialized: %s; last seen: %s\n",
			group.Project.Name, group.Project.InitializedAt.Format("2006-01-02T15:04:05.999999999Z07:00"),
			group.Project.LastSeen.Format("2006-01-02T15:04:05.999999999Z07:00")); err != nil {
			return err
		}
	}
	if projectsApplyFlag {
		_, err = fmt.Fprintf(out, "Original registry backup: %s\n", groups[0].Backup)
	} else {
		_, err = fmt.Fprintln(out, "Registry unchanged. Apply with: codegraph projects --deduplicate --apply")
	}
	return err
}
