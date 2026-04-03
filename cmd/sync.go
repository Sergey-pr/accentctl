package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/sergey-pr/accentctl/internal/api"
	"github.com/sergey-pr/accentctl/internal/config"
	"github.com/sergey-pr/accentctl/internal/output"
)

const chunkSize = 200

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync local files with Accent",
	Long: `Upload local source files to Accent. By default runs a dry-run (peek)
showing what would change. Use --write to apply changes and download
the updated exports.`,
	Example: `  accentctl sync
  accentctl sync --write
  accentctl sync --write --add-translations --merge-type force`,
	RunE: runSync,
}

var (
	syncWrite           bool
	syncAddTranslations bool
	syncType            string
	syncMergeType       string
	syncOrderBy         string
)

func init() {
	syncCmd.Flags().BoolVar(&syncWrite, "write", false, "Apply changes and write exported files locally")
	syncCmd.Flags().BoolVar(&syncAddTranslations, "add-translations", false, "Upload existing translations to Accent")
	syncCmd.Flags().StringVar(&syncType, "sync-type", "smart", "Sync strategy: smart or passive")
	syncCmd.Flags().StringVar(&syncMergeType, "merge-type", "smart", "Merge strategy for add-translations: smart, passive, or force")
	syncCmd.Flags().StringVar(&syncOrderBy, "order-by", "index", "Order of exported keys: index, -index, key, -key, updated, -updated")

}

func runSync(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	client := api.New(cfg.APIURL, cfg.APIKey, verbose)
	output.Section("Syncing files")

	for _, file := range cfg.Files {
		if err = runHooks(file.Hooks.BeforeSync); err != nil {
			return fmt.Errorf("beforeSync hook failed: %w", err)
		}

		if err = syncFile(client, file, !syncWrite, syncType, syncOrderBy); err != nil {
			return err
		}

		if err = runHooks(file.Hooks.AfterSync); err != nil {
			return fmt.Errorf("afterSync hook failed: %w", err)
		}
	}

	if syncAddTranslations {
		output.Section("Adding translations")
		for _, file := range cfg.Files {
			if err = runHooks(file.Hooks.BeforeAddTranslations); err != nil {
				return fmt.Errorf("beforeAddTranslations hook failed: %w", err)
			}
			if err = addTranslationsFile(client, file, !syncWrite, syncMergeType); err != nil {
				return err
			}
			if err = runHooks(file.Hooks.AfterAddTranslations); err != nil {
				return fmt.Errorf("afterAddTranslations hook failed: %w", err)
			}
		}
	}

	if syncWrite {
		output.Section("Exporting updated files")
		for _, file := range cfg.Files {
			if err := exportFile(client, file, syncOrderBy); err != nil {
				return err
			}
		}
	}

	return nil
}

func syncFile(client *api.Client, file config.File, dryRun bool, sType, orderBy string) error {
	sources, err := doublestar.FilepathGlob(file.Source)
	if err != nil {
		return fmt.Errorf("invalid source pattern %q: %w", file.Source, err)
	}
	if len(sources) == 0 {
		return fmt.Errorf("no files matched source pattern %q", file.Source)
	}

	opts := api.SyncOptions{
		DryRun:   dryRun,
		SyncType: sType,
		OrderBy:  orderBy,
	}

	var g errgroup.Group
	for _, src := range sources {
		g.Go(func() error {
			documentPath := documentName(src)
			language := file.Language
			if language == "" {
				language = languageFromPath(filepath.ToSlash(src), file.Target)
			}
			peek, err := client.Sync(src, documentPath, file.Format, language, opts)
			if err != nil {
				return fmt.Errorf("%s: %w", src, err)
			}
			if peek != nil {
				output.FileDryRun(src, peek.NewCount, peek.UpdatedCount, peek.RemovedCount)
			} else {
				output.FileSync(src)
			}
			return nil
		})
	}

	return g.Wait()
}

func addTranslationsFile(client *api.Client, file config.File, dryRun bool, mergeType string) error {
	slugs, err := languageSlugsFromFilesystem(file.Target)
	if err != nil {
		return err
	}

	sources, err := doublestar.FilepathGlob(file.Source)
	if err != nil {
		return fmt.Errorf("invalid source pattern %q: %w", file.Source, err)
	}

	// Derive source language from the first source file if not set in config.
	sourceLanguage := file.Language
	if sourceLanguage == "" && len(sources) > 0 {
		sourceLanguage = languageFromPath(filepath.ToSlash(sources[0]), file.Target)
	}

	for _, slug := range slugs {
		if slug == sourceLanguage {
			continue
		}
		for _, src := range sources {
			docPath := documentName(src)
			localPath := applyTargetTemplate(file.Target, slug, docPath)
			if _, err := os.Stat(localPath); err != nil {
				continue
			}

			if dryRun {
				opts := api.AddTranslationsOptions{DryRun: true, MergeType: mergeType}
				peek, err := client.AddTranslations(localPath, docPath, file.Format, slug, opts)
				if errors.Is(err, api.ErrNotFound) {
					continue
				}
				if err != nil {
					return fmt.Errorf("%s: %w", localPath, err)
				}
				if peek != nil {
					output.FileDryRun(localPath, peek.NewCount, peek.UpdatedCount, peek.RemovedCount)
				}
				continue
			}

			if err := addTranslationsChunked(client, localPath, docPath, file.Format, slug, mergeType); err != nil {
				return err
			}
		}
	}

	return nil
}

// addTranslationsForNewKeys force-pushes translations for keys that were just
// added to the source language. Accent auto-creates empty entries for new
// source keys in all languages, so a normal new-key diff finds nothing — we
// must target these keys explicitly using force merge.
func addTranslationsForNewKeys(client *api.Client, file config.File, newKeySet map[string]bool) error {
	slugs, err := languageSlugsFromFilesystem(file.Target)
	if err != nil {
		return err
	}
	sources, err := doublestar.FilepathGlob(file.Source)
	if err != nil {
		return fmt.Errorf("invalid source pattern %q: %w", file.Source, err)
	}
	sourceLanguage := file.Language
	if sourceLanguage == "" && len(sources) > 0 {
		sourceLanguage = languageFromPath(filepath.ToSlash(sources[0]), file.Target)
	}

	for _, slug := range slugs {
		if slug == sourceLanguage {
			continue
		}
		for _, src := range sources {
			docPath := documentName(src)
			localPath := applyTargetTemplate(file.Target, slug, docPath)
			if _, err := os.Stat(localPath); err != nil {
				continue
			}

			data, err := os.ReadFile(localPath)
			if err != nil {
				return fmt.Errorf("%s: %w", localPath, err)
			}
			obj, err := parseJSONObject(data)
			if err != nil || obj == nil {
				continue
			}

			// Keep only leaves whose path is in the new-key set.
			var targetLeaves []leafEntry
			for _, l := range collectLeaves(obj, nil) {
				if newKeySet[leafKey(l.path)] {
					targetLeaves = append(targetLeaves, l)
				}
			}
			if len(targetLeaves) == 0 {
				output.Info(fmt.Sprintf("%s: no new translations", localPath))
				continue
			}

			nChunks := (len(targetLeaves) + chunkSize - 1) / chunkSize
			output.Info(fmt.Sprintf("%s: %d new translations → %d chunk(s)", localPath, len(targetLeaves), nChunks))

			opts := api.AddTranslationsOptions{MergeType: "force"}
			var tmpFiles []string
			for i := 0; i < len(targetLeaves); i += chunkSize {
				if i > 0 {
					time.Sleep(time.Second)
				}
				end := i + chunkSize
				if end > len(targetLeaves) {
					end = len(targetLeaves)
				}
				// Cumulative so earlier chunks aren't absent from later ones.
				chunkData, err := marshalTree(buildTree(targetLeaves[:end]))
				if err != nil {
					return err
				}
				tmp, err := os.CreateTemp("", "accentctl-trans-*.json")
				if err != nil {
					return err
				}
				if _, err := tmp.Write(chunkData); err != nil {
					tmp.Close()
					return err
				}
				tmp.Close()
				tmpFiles = append(tmpFiles, tmp.Name())

				chunkNum := i/chunkSize + 1
				output.Info(fmt.Sprintf("chunk %d/%d: %s", chunkNum, nChunks, tmp.Name()))
				_, err = client.AddTranslations(tmp.Name(), docPath, file.Format, slug, opts)
				if errors.Is(err, api.ErrNotFound) {
					break
				}
				if err != nil {
					return fmt.Errorf("%s chunk %d/%d: %w", localPath, chunkNum, nChunks, err)
				}
				output.FileAddTranslations(fmt.Sprintf("%s [chunk %d/%d]", localPath, chunkNum, nChunks))
			}
			for _, p := range tmpFiles {
				os.Remove(p)
			}
		}
	}
	return nil
}

func addTranslationsChunked(client *api.Client, localPath, docPath, format, language, mergeType string) error {
	var existing []byte
	if mergeType != "force" {
		var err error
		existing, err = client.ExportBytes(docPath, format, language)
		if err != nil {
			return fmt.Errorf("%s: could not fetch existing translations: %w", localPath, err)
		}
	}
	// With force: pass nil so all local keys are treated as new and uploaded.

	chunks, newCount, err := newKeysChunks(localPath, existing, chunkSize)
	if err != nil {
		return fmt.Errorf("%s: chunking failed: %w", localPath, err)
	}

	defer func() {
		for _, p := range chunks {
			if p != localPath {
				os.Remove(p)
			}
		}
	}()

	if len(chunks) == 0 {
		output.Info(fmt.Sprintf("%s: no new translations", localPath))
		return nil
	}

	output.Info(fmt.Sprintf("%s: %d new translations → %d chunk(s)", localPath, newCount, len(chunks)))

	opts := api.AddTranslationsOptions{MergeType: mergeType}
	for i, chunk := range chunks {
		if i > 0 {
			time.Sleep(time.Second)
		}
		output.Info(fmt.Sprintf("chunk %d/%d: %s", i+1, len(chunks), chunk))
		_, err := client.AddTranslations(chunk, docPath, format, language, opts)
		if errors.Is(err, api.ErrNotFound) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%s chunk %d/%d: %w", localPath, i+1, len(chunks), err)
		}
		output.FileAddTranslations(fmt.Sprintf("%s [chunk %d/%d]", localPath, i+1, len(chunks)))
	}
	return nil
}
