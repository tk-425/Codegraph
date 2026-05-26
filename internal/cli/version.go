package cli

// Version information (set via ldflags during build)
var (
	Version = "0.1.0"
)

func init() {
	rootCmd.Version = Version
	rootCmd.SetVersionTemplate("codegraph version {{.Version}}\n")
}
