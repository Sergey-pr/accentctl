package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sergey-pr/accentctl/internal/api"
	"github.com/sergey-pr/accentctl/internal/config"
)

var verbose bool

var root = &cobra.Command{
	Use:   "accentctl",
	Short: "A CLI tool for Accent, the translation management platform",
	Long: `accentctl lets you sync, pull, and manage translations
via the Accent API (https://www.accent.reviews/).

Configuration is read from accent.json (or accent.yaml / accent.toml)
in the current directory. The ACCENT_API_KEY and ACCENT_API_URL
environment variables override the values in the config file.`,
	PersistentPreRun: func(cmd *cobra.Command, _ []string) {
		cmd.SilenceUsage = true
	},
}

func Execute(version string) {
	root.Version = version
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func newClient(cfg *config.Config) *api.Client {
	return api.New(cfg.APIURL, cfg.APIKey, verbose, cfg.RequestDelay)
}

// serverExport returns a document's current bytes, treating a missing document
// (ErrNotFound) as empty since sync, cleanup and status all diff against zero keys.
func serverExport(client *api.Client, documentPath, format, language string) ([]byte, error) {
	data, err := client.ExportBytes(documentPath, format, language)
	if errors.Is(err, api.ErrNotFound) {
		return nil, nil
	}
	return data, err
}

// requireJSONFormat rejects configs that sync, cleanup and status cannot honour:
// they parse local files as JSON whatever the configured format, and only pull streams bytes untouched.
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
