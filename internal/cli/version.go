package cli

var (
	Version = "dev"
)

func init() {
	rootCmd.Version = Version
	rootCmd.SetVersionTemplate("codegraph version {{.Version}}\n")
}
