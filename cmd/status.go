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
			doc := helpers.SourceDocument(file, src)
			toPush, toDelete, err := diffWithAccent(client, doc)
			if err != nil {
				return err
			}
			printFileStatus(doc.LocalPath, doc.Language, toPush, toDelete)
		}

		err = helpers.ForEachTranslationFile(file, func(doc helpers.Document) error {
			toPush, toDelete, err := diffWithAccent(client, doc)
			if err != nil {
				return err
			}
			printFileStatus(doc.LocalPath, doc.Language, toPush, toDelete)
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
func diffWithAccent(client *api.Client, doc helpers.Document) (toPush, toDelete int, err error) {
	existingData, err := serverExport(client, doc)
	if err != nil {
		return 0, 0, fmt.Errorf("%s: %w", doc.LocalPath, err)
	}

	localObj, err := helpers.ReadJSONObjectFile(doc.LocalPath)
	if err != nil {
		return 0, 0, err
	}
	var serverNodes []helpers.NodeEntry
	if len(existingData) > 0 {
		accObj, err := helpers.ParseJSONObject(existingData)
		if err != nil {
			output.Info(fmt.Sprintf("%s: skipping malformed server response: %v", doc.LocalPath, err))
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
