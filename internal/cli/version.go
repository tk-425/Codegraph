package cli

import (
	"fmt"
)

// Version information (set via ldflags during build)
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

func init() {
	rootCmd.Version = Version
	rootCmd.SetVersionTemplate(fmt.Sprintf("codegraph version %s (commit: %s, built: %s)\n", Version, GitCommit, BuildDate))
}
