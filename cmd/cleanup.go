package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/spf13/cobra"

	"github.com/sergey-pr/accentctl/internal/api"
	"github.com/sergey-pr/accentctl/internal/config"
	"github.com/sergey-pr/accentctl/internal/output"
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove Accent keys that are no longer in local source files",
	Long: `Uploads each source file in cumulative chunks using smart sync.
Orphaned keys (present in Accent but not in the local file) are deleted.
Always writes — no dry-run.`,
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
			documentPath := documentName(src)
			language := file.Language
			if language == "" {
				language = languageFromPath(filepath.ToSlash(src), file.Target)
			}
			if err := cleanupFileChunked(client, src, documentPath, file.Format, language); err != nil {
				return err
			}
		}
	}

	output.Section("Exporting updated files")
	for _, file := range cfg.Files {
		if err := exportFile(client, file, "index"); err != nil {
			return err
		}
	}

	return nil
}

// deleteAllKeysChunked deletes every key in Accent for the given document by
// uploading progressively smaller files, each removing 200 keys via smart sync.
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

	accObj, err := parseJSONObject(existingData)
	if err != nil || accObj == nil {
		output.Info(fmt.Sprintf("%s: no keys on server", src))
		return nil
	}
	allLeaves := collectLeaves(accObj, nil)
	if len(allLeaves) == 0 {
		output.Info(fmt.Sprintf("%s: no keys on server", src))
		return nil
	}

	total := len(allLeaves)
	// +1 for the final empty-file chunk
	nChunks := (total+chunkSize-1)/chunkSize + 1
	output.Info(fmt.Sprintf("%s: deleting %d keys in %d chunk(s)", src, total, nChunks))

	opts := api.SyncOptions{SyncType: "smart"}
	var tmpFiles []string
	defer func() {
		for _, p := range tmpFiles {
			os.Remove(p)
		}
	}()

	chunkNum := 0
	for start := 0; start <= total; start += chunkSize {
		if start > 0 {
			time.Sleep(time.Second)
		}
		chunkNum++

		// Upload allLeaves[start+chunkSize:] — drops the current batch of 200.
		// When start >= total the remaining slice is empty → uploads "{}".
		end := start + chunkSize
		if end > total {
			end = total
		}
		remaining := allLeaves[end:]

		data, err := marshalTree(buildTree(remaining))
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
		tmpFiles = append(tmpFiles, tmp.Name())

		output.Info(fmt.Sprintf("chunk %d/%d: %s", chunkNum, nChunks, tmp.Name()))
		_, err = client.Sync(tmp.Name(), documentPath, format, language, opts)
		if err != nil {
			return fmt.Errorf("%s chunk %d/%d: %w", src, chunkNum, nChunks, err)
		}
		output.FileSync(fmt.Sprintf("%s [chunk %d/%d]", src, chunkNum, nChunks))

		if end >= total {
			break
		}
	}
	return nil
}

// cleanupFileChunked removes orphaned keys (present in Accent but not in the
// local file) 200 at a time. Each upload contains: all local keys + remaining
// orphaned keys not yet removed. With smart sync this removes exactly the
// orphaned keys that were dropped from the file.
//
// File size shrinks by chunkSize each round, avoiding 502 on large files.
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

	localObj, err := parseJSONObject(localData)
	if err != nil || localObj == nil {
		return fmt.Errorf("%s: not a JSON object", src)
	}
	localLeaves := collectLeaves(localObj, nil)

	// Build set of local leaf keys.
	localSet := make(map[string]bool, len(localLeaves))
	for _, l := range localLeaves {
		localSet[leafKey(l.path)] = true
	}

	// Find orphaned leaves (in Accent but not in local file).
	var orphaned []leafEntry
	if len(existingData) > 0 {
		accObj, err := parseJSONObject(existingData)
		if err == nil && accObj != nil {
			for _, l := range collectLeaves(accObj, nil) {
				if !localSet[leafKey(l.path)] {
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
	nChunks := (total + chunkSize - 1) / chunkSize
	output.Info(fmt.Sprintf("%s: removing %d orphaned keys in %d chunk(s)", src, total, nChunks))

	opts := api.SyncOptions{SyncType: "smart"}
	var tmpFiles []string
	defer func() {
		for _, p := range tmpFiles {
			os.Remove(p)
		}
	}()

	for i := 0; i < total; i += chunkSize {
		if i > 0 {
			time.Sleep(time.Second)
		}

		// This chunk removes orphaned[i : i+chunkSize].
		// File = local keys + orphaned keys not yet removed (i+chunkSize onward).
		end := i + chunkSize
		if end > total {
			end = total
		}
		remaining := orphaned[end:]
		combined := append(localLeaves, remaining...)

		data, err := marshalTree(buildTree(combined))
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
		tmpFiles = append(tmpFiles, tmp.Name())

		chunkNum := i/chunkSize + 1
		output.Info(fmt.Sprintf("chunk %d/%d: %s", chunkNum, nChunks, tmp.Name()))
		_, err = client.Sync(tmp.Name(), documentPath, format, language, opts)
		if err != nil {
			return fmt.Errorf("%s chunk %d/%d: %w", src, chunkNum, nChunks, err)
		}
		output.FileSync(fmt.Sprintf("%s [chunk %d/%d]", src, chunkNum, nChunks))
	}
	return nil
}
