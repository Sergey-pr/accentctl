package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var verbose bool

var root = &cobra.Command{
	Use:   "accentctl",
	Short: "A CLI tool for Accent- the translation management platform",
	Long: `accentctl lets you sync, export, and manage translations
via the Accent API (https://www.accent.reviews/).

Configuration is read from accent.json (or accent.yaml / accent.toml)
in the current directory. The ACCENT_API_KEY and ACCENT_API_URL
environment variables override the values in the config file.`,
}

func Execute() {
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Log HTTP requests and responses")

	root.AddCommand(exportCmd)
	root.AddCommand(syncCmd)
	root.AddCommand(updateCmd)
	root.AddCommand(cleanupCmd)
	root.AddCommand(statsCmd)
	root.AddCommand(initCmd)
	root.AddCommand(keyCmd)
}
