package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/sergey-pr/accentctl/internal/api"
	"github.com/sergey-pr/accentctl/internal/config"
	"github.com/sergey-pr/accentctl/internal/constants"
	"github.com/sergey-pr/accentctl/internal/output"
	"github.com/sergey-pr/accentctl/internal/utils"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Add new keys to Accent and force their translations",
	Long: `Uploads new source keys in chunks and force-pushes translations for
those new keys to all target languages.

With --force: uploads all source keys and force-pushes all translations
for all languages.`,
	Example: `  accentctl sync
  accentctl sync --force
  accentctl sync --order-by key`,
	RunE: runSync,
}

var (
	syncOrderBy string
	syncForce   bool
)

func init() {
	syncCmd.Flags().StringVar(&syncOrderBy, "order-by", "key", "Order of pulled keys: index, -index, key, -key, updated, -updated")
	syncCmd.Flags().BoolVar(&syncForce, "force", false, "Upload all source keys and force all translations for all languages")
}

func runSync(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	client := api.New(cfg.APIURL, cfg.APIKey, verbose)

	for _, file := range cfg.Files {
		if err := runHooks(file.Hooks.BeforeSync); err != nil {
			return fmt.Errorf("beforeSync hook failed: %w", err)
		}
	}

	output.Section("Syncing files")

	type fileNewKeys struct {
		file   config.File
		keySet map[string]bool
	}
	var results []fileNewKeys

	for _, file := range cfg.Files {
		sources, err := file.Sources()
		if err != nil {
			return err
		}

		keySet := map[string]bool{}
		for _, src := range sources {
			documentPath := utils.DocumentName(src)
			language := file.Language
			if language == "" {
				language = utils.LanguageFromPath(filepath.ToSlash(src), file.Target)
			}

			if syncForce {
				if err := deleteAllKeysChunked(client, src, documentPath, file.Format, language); err != nil {
					return err
				}
			}

			newLeaves, err := syncFileChunked(client, src, documentPath, file.Format, language, syncOrderBy, syncForce)
			if err != nil {
				return err
			}
			for _, l := range newLeaves {
				keySet[utils.LeafKey(l.Path)] = true
			}
		}
		results = append(results, fileNewKeys{file, keySet})
	}

	output.Section("Adding translations")
	for _, r := range results {
		if syncForce {
			// Force all translations for all languages.
			if err := addTranslationsFile(client, r.file, "force"); err != nil {
				return err
			}
		} else if len(r.keySet) > 0 {
			// Force translations only for the newly added source keys.
			if err := addTranslationsForNewKeys(client, r.file, r.keySet); err != nil {
				return err
			}
		}
	}

	output.Section("Pulling updated files")
	for _, file := range cfg.Files {
		if err := pullFile(client, file, syncOrderBy); err != nil {
			return err
		}
	}

	for _, file := range cfg.Files {
		if err := runHooks(file.Hooks.AfterSync); err != nil {
			return fmt.Errorf("afterSync hook failed: %w", err)
		}
	}

	return nil
}

// syncFileChunked fetches the current Accent state, finds new leaf keys, and
// uploads them in batches of ChunkSize using passive sync.
// With force=true, treats all local keys as new (re-uploads everything).
// Returns the uploaded leaf entries so callers can push their translations.
func syncFileChunked(client *api.Client, src, documentPath, format, language, orderBy string, force bool) ([]utils.LeafEntry, error) {
	var existing []byte
	if !force {
		var err error
		existing, err = client.ExportBytes(documentPath, format, language)
		if err != nil {
			return nil, fmt.Errorf("%s: could not fetch existing keys: %w", src, err)
		}
	}

	chunks, newLeaves, err := utils.NewKeysChunksWithLeaves(src, existing, constants.ChunkSize)
	if err != nil {
		return nil, fmt.Errorf("%s: chunking failed: %w", src, err)
	}

	defer func() {
		for _, p := range chunks {
			if p != src {
				os.Remove(p)
			}
		}
	}()

	if len(chunks) == 0 {
		output.Info(fmt.Sprintf("%s: no new keys", src))
		return nil, nil
	}

	output.Info(fmt.Sprintf("%s: %d keys -> %d chunk(s)", src, len(newLeaves), len(chunks)))

	opts := api.SyncOptions{SyncType: "passive", OrderBy: orderBy}

	output.Section(fmt.Sprintf("Syncing %s - %d chunk(s)", src, len(chunks)))
	for i, chunk := range chunks {
		if verbose {
			output.Info(fmt.Sprintf("chunk %d/%d: %s", i+1, len(chunks), chunk))
		}
		_, err := client.Sync(chunk, documentPath, format, language, opts)
		if err != nil {
			return nil, fmt.Errorf("%s chunk %d/%d: %w", src, i+1, len(chunks), err)
		}
		if verbose {
			output.FileSync(fmt.Sprintf("%s [chunk %d/%d]", src, i+1, len(chunks)))
		} else {
			output.ChunkProgress(src, i+1, len(chunks))
		}
	}
	return newLeaves, nil
}
