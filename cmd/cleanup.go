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

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove Accent keys that are no longer in local source files",
	Long: `Uploads each source file in cumulative chunks using smart sync.
Orphaned keys (present in Accent but not in the local file) are deleted.`,
	Example: `  accentctl cleanup`,
	RunE:    runCleanup,
}

func runCleanup(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	client := api.New(cfg.APIURL, cfg.APIKey, verbose)
	output.Section("Cleaning up")

	for _, file := range cfg.Files {
		sources, err := file.Sources()
		if err != nil {
			return err
		}

		for _, src := range sources {
			documentPath := helpers.DocumentName(src)
			language := file.Language
			if language == "" {
				language = helpers.LanguageFromPath(filepath.ToSlash(src), file.Target)
			}
			if err := cleanupFileChunked(client, src, documentPath, file.Format, language); err != nil {
				return err
			}
		}
	}

	output.Section("Pulling updated files")
	for _, file := range cfg.Files {
		if err := pullFile(client, file, "index"); err != nil {
			return err
		}
	}

	return nil
}

// cleanupFileChunked removes orphaned keys by chunks.
// Each upload contains: all local keys + remaining
// orphaned keys not yet removed. With smart sync this removes exactly the
// orphaned keys that were dropped from the file.
//
// File size shrinks by constants.ChunkSize each round
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

	localObj, err := helpers.ParseJSONObject(localData)
	if err != nil || localObj == nil {
		return fmt.Errorf("%s: not a JSON object", src)
	}
	localNodes := helpers.CollectNodes(localObj, nil)

	// Build set of local node keys.
	localSet := make(map[string]bool, len(localNodes))
	for _, l := range localNodes {
		localSet[helpers.NodeKey(l.Path)] = true
	}

	// Find orphaned nodes (in Accent but not in local file).
	var orphaned []helpers.NodeEntry
	if len(existingData) > 0 {
		accObj, err := helpers.ParseJSONObject(existingData)
		if err == nil && accObj != nil {
			for _, l := range helpers.CollectNodes(accObj, nil) {
				if !localSet[helpers.NodeKey(l.Path)] {
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
			_ = os.Remove(p)
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
		combined := append(localNodes, remaining...)

		data, err := helpers.MarshalNodes(combined)
		if err != nil {
			return fmt.Errorf("%s: %w", src, err)
		}

		tmp, err := os.CreateTemp("", "accentctl-cleanup-*.json")
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
