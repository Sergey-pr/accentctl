package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sergey-pr/accentctl/internal/api"
	"github.com/sergey-pr/accentctl/internal/config"
	"github.com/sergey-pr/accentctl/internal/helpers"
)

type globalOptions struct {
	verbose bool
}

func newRootCmd(version string) *cobra.Command {
	var global globalOptions
	root := &cobra.Command{
		Use:   "accentctl",
		Short: "A CLI tool for Accent, the translation management platform",
		Long: `accentctl lets you sync, pull, and manage translations
via the Accent API (https://www.accent.reviews/).

Configuration is read from accent.json (or accent.yaml / accent.toml)
in the current directory. The ACCENT_API_KEY and ACCENT_API_URL
environment variables override the values in the config file.`,
		Version: version,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			cmd.SilenceUsage = true
		},
	}
	root.PersistentFlags().BoolVarP(&global.verbose, "verbose", "v", false, "Log HTTP requests and responses")
	root.AddCommand(
		newPullCmd(&global),
		newSyncCmd(&global),
		newCleanupCmd(&global),
		newStatusCmd(&global),
		newInitCmd(),
		newKeyCmd(),
	)
	return root
}

func Execute(version string) {
	if err := newRootCmd(version).Execute(); err != nil {
		os.Exit(1)
	}
}

func newClient(cfg *config.Config, verbose bool) *api.Client {
	return api.New(cfg.APIURL, cfg.APIKey, verbose, cfg.RequestDelay)
}

// serverExport returns a document's current bytes. A missing document (ErrNotFound)
// comes back empty because sync and cleanup treat it as having no keys.
func serverExport(client *api.Client, doc helpers.Document) ([]byte, error) {
	data, err := client.ExportBytes(doc.Path, doc.Format, doc.Language)
	if errors.Is(err, api.ErrNotFound) {
		return nil, nil
	}
	return data, err
}

// requireJSONFormat rejects configs that sync, cleanup and status cannot honour:
// they parse local files as JSON whatever the configured format, and only pull streams bytes untouched.
func requireJSONFormat(cfg *config.Config, command string) error {
	for _, file := range cfg.Files {
		if file.Format != "json" {
			return fmt.Errorf(
				"%s supports format \"json\" only, but %q is configured for source %q; only pull works with other formats",
				command, file.Format, file.Source)
		}
	}
	return nil
}

func requireSourceLanguages(cfg *config.Config) error {
	for _, file := range cfg.Files {
		sources, err := file.Sources()
		if err != nil {
			return err
		}
		for _, src := range sources {
			if helpers.SourceLanguage(file, src) == "" {
				return fmt.Errorf(
					"cannot determine the source language of %s: set \"language\" for this file or make its path match target %q",
					src, file.Target)
			}
		}
	}
	return nil
}
