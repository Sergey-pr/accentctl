package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/sergey-pr/accentctl/internal/api"
	"github.com/sergey-pr/accentctl/internal/config"
	"github.com/sergey-pr/accentctl/internal/output"
	"github.com/sergey-pr/accentctl/internal/utils"
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

	client := api.New(cfg.APIURL, cfg.APIKey, verbose)
	output.Section("Status")

	for _, file := range cfg.Files {
		sources, err := file.Sources()
		if err != nil {
			return err
		}

		slugs, err := utils.LanguageSlugsFromFilesystem(file.Target)
		if err != nil {
			return err
		}

		// Determine source language.
		sourceLanguage := file.Language
		if sourceLanguage == "" && len(sources) > 0 {
			sourceLanguage = utils.LanguageFromPath(filepath.ToSlash(sources[0]), file.Target)
		}

		// Check source files.
		for _, src := range sources {
			docPath := utils.DocumentName(src)
			language := file.Language
			if language == "" {
				language = utils.LanguageFromPath(filepath.ToSlash(src), file.Target)
			}

			toPush, toDelete, err := diffWithAccent(client, src, docPath, file.Format, language)
			if err != nil {
				return err
			}
			printFileStatus(src, language, toPush, toDelete)
		}

		// Check translation files for each slug.
		for _, slug := range slugs {
			if slug == sourceLanguage {
				continue
			}
			for _, src := range sources {
				docPath := utils.DocumentName(src)
				localPath := utils.ApplyTargetTemplate(file.Target, slug, docPath)
				if _, err := os.Stat(localPath); err != nil {
					continue
				}

				toPush, toDelete, err := diffWithAccent(client, localPath, docPath, file.Format, slug)
				if err != nil {
					return err
				}
				printFileStatus(localPath, slug, toPush, toDelete)
			}
		}
	}

	return nil
}

// diffWithAccent compares a local file against the Accent server state.
// Returns the number of keys that need pushing (local but not in Accent)
// and the number that need deleting (in Accent but not local).
func diffWithAccent(client *api.Client, localPath, docPath, format, language string) (toPush, toDelete int, err error) {
	existingData, err := client.ExportBytes(docPath, format, language)
	if err != nil {
		return 0, 0, fmt.Errorf("%s: %w", localPath, err)
	}

	localData, err := os.ReadFile(localPath)
	if err != nil {
		return 0, 0, fmt.Errorf("%s: %w", localPath, err)
	}

	localObj, err := utils.ParseJSONObject(localData)
	if err != nil {
		output.Info(fmt.Sprintf("%s: skipping malformed JSON: %v", localPath, err))
		return 0, 0, nil
	}
	var localSet map[string]bool
	var localLeaves []utils.LeafEntry
	if localObj != nil {
		localLeaves = utils.CollectLeaves(localObj, nil)
		localSet = make(map[string]bool, len(localLeaves))
		for _, l := range localLeaves {
			localSet[utils.LeafKey(l.Path)] = true
		}
	}

	accentSet := map[string]bool{}
	if len(existingData) > 0 {
		accObj, err := utils.ParseJSONObject(existingData)
		if err != nil {
			output.Info(fmt.Sprintf("%s: skipping malformed server response: %v", localPath, err))
			return 0, 0, nil
		}
		if accObj != nil {
			for _, l := range utils.CollectLeaves(accObj, nil) {
				accentSet[utils.LeafKey(l.Path)] = true
			}
		}
	}

	for _, l := range localLeaves {
		if !accentSet[utils.LeafKey(l.Path)] {
			toPush++
		}
	}
	for k := range accentSet {
		if !localSet[k] {
			toDelete++
		}
	}
	return toPush, toDelete, nil
}

func printFileStatus(path, language string, toPush, toDelete int) {
	fmt.Printf("\n  %s  (%s)\n", path, language)
	fmt.Printf("    to push:   %d\n", toPush)
	fmt.Printf("    to delete: %d\n", toDelete)
}
