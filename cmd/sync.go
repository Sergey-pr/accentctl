package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/sergey-pr/accentctl/internal/api"
	"github.com/sergey-pr/accentctl/internal/config"
	"github.com/sergey-pr/accentctl/internal/constants"
	"github.com/sergey-pr/accentctl/internal/helpers"
	"github.com/sergey-pr/accentctl/internal/output"
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
			documentPath := helpers.DocumentName(src)
			language := file.Language
			if language == "" {
				language = helpers.LanguageFromPath(filepath.ToSlash(src), file.Target)
			}

			if syncForce {
				if err := deleteAllKeysChunked(client, src, documentPath, file.Format, language); err != nil {
					return err
				}
			}

			newNodes, err := syncFileChunked(client, src, documentPath, file.Format, language, syncOrderBy, syncForce)
			if err != nil {
				return err
			}
			for _, l := range newNodes {
				keySet[helpers.NodeKey(l.Path)] = true
			}
		}
		results = append(results, fileNewKeys{file, keySet})
	}

	output.Section("Adding translations")
	for _, r := range results {
		if syncForce {
			// Force all translations for all languages.
			if err := helpers.AddTranslationsFile(client, r.file, "force", verbose); err != nil {
				return err
			}
		} else if len(r.keySet) > 0 {
			// Force translations only for the newly added source keys.
			if err := helpers.AddTranslationsForNewKeys(client, r.file, r.keySet, verbose); err != nil {
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

// deleteAllKeysChunked deletes every key in Accent for the given document by
// uploading progressively smaller files, each removing chunk size via smart sync.
// The final upload is an empty object which clears all remaining keys.
func deleteAllKeysChunked(client *api.Client, src, documentPath, format, language string) error {
	existingData, err := client.ExportBytes(documentPath, format, language)
	if err != nil {
		return fmt.Errorf("%s: could not fetch existing keys: %w", src, err)
	}
	if len(existingData) == 0 {
		output.Info(fmt.Sprintf("%s: no keys on server", src))
		return nil
	}

	accObj, err := helpers.ParseJSONObject(existingData)
	if err != nil || accObj == nil {
		output.Info(fmt.Sprintf("%s: no keys on server", src))
		return nil
	}
	allNodes := helpers.CollectNodes(accObj, nil)
	if len(allNodes) == 0 {
		output.Info(fmt.Sprintf("%s: no keys on server", src))
		return nil
	}

	total := len(allNodes)
	// +1 for the final empty-file chunk
	nChunks := (total + constants.ChunkSize - 1) / constants.ChunkSize
	output.Info(fmt.Sprintf("%s: deleting %d keys in %d chunk(s)", src, total, nChunks))

	opts := api.SyncOptions{SyncType: "smart"}
	var tmpFiles []string
	defer func() {
		for _, p := range tmpFiles {
			_ = os.Remove(p)
		}
	}()

	chunkNum := 0
	for start := 0; start <= total; start += constants.ChunkSize {
		chunkNum++

		// Upload allNodes[start+constants.ChunkSize:]
		// When start >= total the remaining slice is empty and uploads "{}"
		end := start + constants.ChunkSize
		if end > total {
			end = total
		}
		remaining := allNodes[end:]

		data, err := helpers.MarshalNodes(remaining)
		if err != nil {
			return fmt.Errorf("%s: %w", src, err)
		}

		tmp, err := os.CreateTemp("", "accentctl-del-*.json")
		if err != nil {
			return err
		}
		if _, err := tmp.Write(data); err != nil {
			_ = tmp.Close()
			return err
		}
		_ = tmp.Close()

		tmpName := tmp.Name()

		tmpFiles = append(tmpFiles, tmpName)

		if verbose {
			output.Info(fmt.Sprintf("chunk %d/%d: %s", chunkNum, nChunks, tmpName))
		}
		err = syncChunk(client, src, documentPath, format, language, tmpName, chunkNum, nChunks, opts)
		if err != nil {
			return err
		}

		if end >= total {
			break
		}
	}
	return nil
}

// syncFileChunked fetches the current Accent state, finds new keys, and
// uploads them in batches of ChunkSize using passive sync.
// With force=true, treats all local keys as new (re-uploads everything).
func syncFileChunked(client *api.Client, src, documentPath, format, language, orderBy string, force bool) ([]helpers.NodeEntry, error) {
	var existing []byte
	if !force {
		var err error
		existing, err = client.ExportBytes(documentPath, format, language)
		if err != nil {
			return nil, fmt.Errorf("%s: could not fetch existing keys: %w", src, err)
		}
	}

	chunks, newNodes, err := helpers.NewKeysChunksWithNodes(src, existing, constants.ChunkSize)
	if err != nil {
		return nil, fmt.Errorf("%s: chunking failed: %w", src, err)
	}

	defer func() {
		for _, p := range chunks {
			if p != src {
				_ = os.Remove(p)
			}
		}
	}()

	if len(chunks) == 0 {
		output.Info(fmt.Sprintf("%s: no new keys", src))
		return nil, nil
	}

	output.Info(fmt.Sprintf("%s: %d keys -> %d chunk(s)", src, len(newNodes), len(chunks)))

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
	return newNodes, nil
}
