package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/spf13/cobra"

	"github.com/sergey-pr/accentctl/internal/api"
	"github.com/sergey-pr/accentctl/internal/config"
	"github.com/sergey-pr/accentctl/internal/constants"
	"github.com/sergey-pr/accentctl/internal/output"
	"github.com/sergey-pr/accentctl/internal/utils"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove Accent keys that are no longer in local source files",
	Long: `Uploads each source file in cumulative chunks using smart sync.
Orphaned keys (present in Accent but not in the local file) are deleted.`,
	Example: `  accentctl cleanup`,
	RunE:    runCleanup,
}

func runCleanup(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	client := api.New(cfg.APIURL, cfg.APIKey, verbose)
	output.Section("Cleaning up")

	for _, file := range cfg.Files {
		sources, err := doublestar.FilepathGlob(file.Source)
		if err != nil {
			return fmt.Errorf("invalid source pattern %q: %w", file.Source, err)
		}
		if len(sources) == 0 {
			return fmt.Errorf("no files matched source pattern %q", file.Source)
		}

		for _, src := range sources {
			documentPath := utils.DocumentName(src)
			language := file.Language
			if language == "" {
				language = utils.LanguageFromPath(filepath.ToSlash(src), file.Target)
			}
			if err := cleanupFileChunked(client, src, documentPath, file.Format, language); err != nil {
				return err
			}
		}
	}

	output.Section("Exporting updated files")
	for _, file := range cfg.Files {
		if err := pullFile(client, file, "index"); err != nil {
			return err
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

	accObj, err := utils.ParseJSONObject(existingData)
	if err != nil || accObj == nil {
		output.Info(fmt.Sprintf("%s: no keys on server", src))
		return nil
	}
	allLeaves := utils.CollectLeaves(accObj, nil)
	if len(allLeaves) == 0 {
		output.Info(fmt.Sprintf("%s: no keys on server", src))
		return nil
	}

	total := len(allLeaves)
	// +1 for the final empty-file chunk
	nChunks := (total + constants.ChunkSize - 1) / constants.ChunkSize
	output.Info(fmt.Sprintf("%s: deleting %d keys in %d chunk(s)", src, total, nChunks))

	opts := api.SyncOptions{SyncType: "smart"}
	var tmpFiles []string
	defer func() {
		for _, p := range tmpFiles {
			os.Remove(p)
		}
	}()

	chunkNum := 0
	for start := 0; start <= total; start += constants.ChunkSize {
		chunkNum++

		// Upload allLeaves[start+constants.ChunkSize:].
		// When start >= total the remaining slice is empty → uploads "{}".
		end := start + constants.ChunkSize
		if end > total {
			end = total
		}
		remaining := allLeaves[end:]

		data, err := utils.MarshalLeaves(remaining)
		if err != nil {
			return fmt.Errorf("%s: %w", src, err)
		}

		tmp, err := os.CreateTemp("", "accentctl-del-*.json")
		if err != nil {
			return err
		}
		if _, err := tmp.Write(data); err != nil {
			tmp.Close()
			return err
		}
		tmp.Close()

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

// cleanupFileChunked removes orphaned keys by chunks.
// Each upload contains: all local keys + remaining
// orphaned keys not yet removed. With smart sync this removes exactly the
// orphaned keys that were dropped from the file.
//
// File size shrinks by constants.ChunkSize each round, avoiding server OOM on big files.
func cleanupFileChunked(client *api.Client, src, documentPath, format, language string) error {
	// Fetch what Accent currently has.
	existingData, err := client.ExportBytes(documentPath, format, language)
	if err != nil {
		return fmt.Errorf("%s: could not fetch existing keys: %w", src, err)
	}

	localData, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("%s: %w", src, err)
	}

	localObj, err := utils.ParseJSONObject(localData)
	if err != nil || localObj == nil {
		return fmt.Errorf("%s: not a JSON object", src)
	}
	localLeaves := utils.CollectLeaves(localObj, nil)

	// Build set of local leaf keys.
	localSet := make(map[string]bool, len(localLeaves))
	for _, l := range localLeaves {
		localSet[utils.LeafKey(l.Path)] = true
	}

	// Find orphaned leaves (in Accent but not in local file).
	var orphaned []utils.LeafEntry
	if len(existingData) > 0 {
		accObj, err := utils.ParseJSONObject(existingData)
		if err == nil && accObj != nil {
			for _, l := range utils.CollectLeaves(accObj, nil) {
				if !localSet[utils.LeafKey(l.Path)] {
					orphaned = append(orphaned, l)
				}
			}
		}
	}

	if len(orphaned) == 0 {
		output.Info(fmt.Sprintf("%s: no orphaned keys", src))
		return nil
	}

	total := len(orphaned)
	nChunks := (total + constants.ChunkSize - 1) / constants.ChunkSize
	output.Info(fmt.Sprintf("%s: removing %d orphaned keys in %d chunk(s)", src, total, nChunks))

	opts := api.SyncOptions{SyncType: "smart"}
	var tmpFiles []string
	defer func() {
		for _, p := range tmpFiles {
			os.Remove(p)
		}
	}()

	for i := 0; i < total; i += constants.ChunkSize {
		// This chunk removes orphaned[i : i+constants.ChunkSize].
		// File = local keys + orphaned keys not yet removed (i+constants.ChunkSize onward).
		end := i + constants.ChunkSize
		if end > total {
			end = total
		}
		remaining := orphaned[end:]
		combined := append(localLeaves, remaining...)

		data, err := utils.MarshalLeaves(combined)
		if err != nil {
			return fmt.Errorf("%s: %w", src, err)
		}

		tmp, err := os.CreateTemp("", "accentctl-cleanup-*.json")
		if err != nil {
			return err
		}
		if _, err := tmp.Write(data); err != nil {
			tmp.Close()
			return err
		}
		tmp.Close()

		tmpName := tmp.Name()

		tmpFiles = append(tmpFiles, tmpName)

		chunkNum := i/constants.ChunkSize + 1
		if verbose {
			output.Info(fmt.Sprintf("chunk %d/%d: %s", chunkNum, nChunks, tmpName))
		}

		err = syncChunk(client, src, documentPath, format, language, tmpName, chunkNum, nChunks, opts)
		if err != nil {
			return err
		}

	}
	return nil
}

func syncChunk(client *api.Client, src, documentPath, format, language, tmpName string, chunkNum, nChunks int,
	opts api.SyncOptions) error {
	_, err := client.Sync(tmpName, documentPath, format, language, opts)
	if err != nil {
		return fmt.Errorf("%s chunk %d/%d: %w", src, chunkNum, nChunks, err)
	}
	if verbose {
		output.FileSync(fmt.Sprintf("%s [chunk %d/%d]", src, chunkNum, nChunks))
	} else {
		output.ChunkProgress(src, chunkNum, nChunks)
	}
	return nil
}
