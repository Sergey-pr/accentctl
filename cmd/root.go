package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sergey-pr/accentctl/internal/config"
)

var verbose bool

var root = &cobra.Command{
	Use:   "accentctl",
	Short: "A CLI tool for Accent- the translation management platform",
	Long: `accentctl lets you sync, pull, and manage translations
via the Accent API (https://www.accent.reviews/).

Configuration is read from accent.json (or accent.yaml / accent.toml)
in the current directory. The ACCENT_API_KEY and ACCENT_API_URL
environment variables override the values in the config file.`,
	PersistentPreRun: func(cmd *cobra.Command, _ []string) {
		cmd.SilenceUsage = true
	},
}

func Execute() {
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// requireJSONFormat rejects configs that sync, cleanup and status cannot
// honour. All three read local files with a JSON parser regardless of the
// configured format, so anything else would fail later with a parse error that
// says nothing about the real cause. Only pull is format-agnostic: it streams
// bytes to disk without inspecting them.
//
// An unset format is left alone: it is not a claim that the tool is about to
// break, and the commands already treat those files as JSON.
func requireJSONFormat(cfg *config.Config, command string) error {
	for _, file := range cfg.Files {
		if file.Format != "" && file.Format != "json" {
			return fmt.Errorf(
				"%s supports format \"json\" only, but %q is configured for source %q; only pull works with other formats",
				command, file.Format, file.Source)
		}
	}
	return nil
}

func init() {
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Log HTTP requests and responses")

	root.AddCommand(pullCmd)
	root.AddCommand(syncCmd)
	root.AddCommand(cleanupCmd)
	root.AddCommand(statusCmd)
	root.AddCommand(initCmd)
	root.AddCommand(keyCmd)
}
