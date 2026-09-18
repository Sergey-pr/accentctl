package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/sergey-pr/accentctl/internal/api"
	"github.com/sergey-pr/accentctl/internal/config"
	"github.com/sergey-pr/accentctl/internal/helpers"
	"github.com/sergey-pr/accentctl/internal/output"
)

var statusCmd = &cobra.Command{
	Use:     "status",
	Short:   "Show how many keys need pushing or deleting per language file",
	Example: `  accentctl status`,
	RunE:    runStatus,
}

func runStatus(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if err := requireJSONFormat(cfg, "status"); err != nil {
		return err
	}

	client := newClient(cfg)
	output.Section("Status")

	for _, file := range cfg.Files {
		sources, err := file.Sources()
		if err != nil {
			return err
		}

		for _, src := range sources {
			docPath := helpers.DocumentName(src)
			language := helpers.SourceLanguage(file, src)

			toPush, toDelete, err := diffWithAccent(client, src, docPath, file.Format, language)
			if err != nil {
				return err
			}
			printFileStatus(src, language, toPush, toDelete)
		}

		err = helpers.ForEachTranslationFile(file, func(localPath, docPath, slug string) error {
			toPush, toDelete, err := diffWithAccent(client, localPath, docPath, file.Format, slug)
			if err != nil {
				return err
			}
			printFileStatus(localPath, slug, toPush, toDelete)
			return nil
		})
		if err != nil {
			return err
		}
	}

	return nil
}

// diffWithAccent counts keys to push (local but not in Accent) and keys to
// delete (in Accent but not local) for one file.
func diffWithAccent(client *api.Client, localPath, docPath, format, language string) (toPush, toDelete int, err error) {
	existingData, err := serverExport(client, docPath, format, language)
	if err != nil {
		return 0, 0, fmt.Errorf("%s: %w", localPath, err)
	}

	localObj, err := helpers.ReadJSONObjectFile(localPath)
	if err != nil {
		return 0, 0, err
	}
	var serverNodes []helpers.NodeEntry
	if len(existingData) > 0 {
		accObj, err := helpers.ParseJSONObject(existingData)
		if err != nil {
			output.Info(fmt.Sprintf("%s: skipping malformed server response: %v", localPath, err))
			return 0, 0, nil
		}
		if accObj != nil {
			serverNodes = helpers.CollectNodes(accObj, nil)
		}
	}

	added, removed := helpers.DiffNodes(helpers.CollectNodes(localObj, nil), serverNodes)
	return len(added), len(removed), nil
}

func printFileStatus(path, language string, toPush, toDelete int) {
	fmt.Printf("\n  %s  (%s)\n", path, language)
	fmt.Printf("    to push:   %d\n", toPush)
	fmt.Printf("    to delete: %d\n", toDelete)
}
