package cmd

import (
	"fmt"
	"slices"

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
	Example: `  accentctl cleanup
  accentctl cleanup --order-by index`,
	RunE: runCleanup,
}

var cleanupOrderBy string

func init() {
	cleanupCmd.Flags().StringVar(&cleanupOrderBy,
		"order-by", "key", "Order of pulled keys: index, -index, key, -key, updated, -updated")
}

func runCleanup(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if err := requireJSONFormat(cfg, "cleanup"); err != nil {
		return err
	}

	client := newClient(cfg)
	output.Section("Cleaning up")

	for _, file := range cfg.Files {
		sources, err := file.Sources()
		if err != nil {
			return err
		}

		for _, src := range sources {
			documentPath := helpers.DocumentName(src)
			language := helpers.SourceLanguage(file, src)
			if err := cleanupFileChunked(client, src, documentPath, file.Format, language); err != nil {
				return err
			}
		}
	}

	output.Section("Pulling updated files")
	for _, file := range cfg.Files {
		if err := pullFile(client, file, cleanupOrderBy); err != nil {
			return err
		}
	}

	return nil
}

// cleanupFileChunked deletes keys that exist in Accent but not locally. Each
// smart-sync upload holds all local keys plus the orphans not yet removed, so one chunk of orphans drops per round.
func cleanupFileChunked(client *api.Client, src, documentPath, format, language string) error {
	existingData, err := serverExport(client, documentPath, format, language)
	if err != nil {
		return fmt.Errorf("%s: could not fetch existing keys: %w", src, err)
	}

	localObj, err := helpers.ReadJSONObjectFile(src)
	if err != nil {
		return err
	}
	localNodes := helpers.CollectNodes(localObj, nil)
	localSet := helpers.NodeSet(localNodes)

	var orphaned []helpers.NodeEntry
	for _, n := range helpers.ServerNodes(existingData) {
		if !localSet[helpers.NodeKey(n.Path)] {
			orphaned = append(orphaned, n)
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
	for start := 0; start < total; start += constants.ChunkSize {
		end := min(start+constants.ChunkSize, total)
		combined := slices.Concat(localNodes, orphaned[end:])
		chunkNum := start/constants.ChunkSize + 1

		err := helpers.WithTempNodeFile(src, combined, "accentctl-cleanup-*.json", func(tmpName string) error {
			if verbose {
				output.Info(fmt.Sprintf("chunk %d/%d: %s", chunkNum, nChunks, tmpName))
			}
			return syncChunk(client, src, documentPath, format, language, tmpName, chunkNum, nChunks, opts)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func syncChunk(client *api.Client, src, documentPath, format, language, tmpName string, chunkNum, nChunks int,
	opts api.SyncOptions) error {
	err := client.Sync(tmpName, documentPath, format, language, opts)
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
