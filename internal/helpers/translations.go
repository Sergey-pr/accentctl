package helpers

import (
	"errors"
	"fmt"
	"os"

	"github.com/sergey-pr/accentctl/internal/api"
	"github.com/sergey-pr/accentctl/internal/config"
	"github.com/sergey-pr/accentctl/internal/constants"
	"github.com/sergey-pr/accentctl/internal/output"
)

// ForEachTranslationFile calls fn for every target-language file that exists
// locally, skipping the source language.
func ForEachTranslationFile(file config.File, fn func(doc Document) error) error {
	slugs, err := LanguageSlugsFromFilesystem(file.Target)
	if err != nil {
		return err
	}
	sources, err := file.Sources()
	if err != nil {
		return err
	}
	sourceLanguage := SourceLanguage(file, sources[0])

	for _, slug := range slugs {
		if slug == sourceLanguage {
			continue
		}
		for _, src := range sources {
			localPath := ApplyTargetTemplate(file.Target, slug, src)
			if _, err := os.Stat(localPath); err != nil {
				continue
			}
			if err := fn(Document{LocalPath: localPath, Path: DocumentName(src), Format: file.Format, Language: slug}); err != nil {
				return err
			}
		}
	}
	return nil
}

// AddAllTranslations pushes every local translation without diffing against the
// server. A "passive" merge keeps reviewer-corrected strings, so recovery cannot clobber them.
func AddAllTranslations(client *api.Client, file config.File, mergeType string, verbose bool) error {
	return ForEachTranslationFile(file, func(doc Document) error {
		obj, err := ReadJSONObjectFile(doc.LocalPath)
		if err != nil {
			return err
		}
		nodes := CollectNodes(obj, nil)
		if len(nodes) == 0 {
			output.Info(fmt.Sprintf("%s: no translations", doc.LocalPath))
			return nil
		}
		return uploadTranslationChunks(client, nodes, doc, mergeType, verbose)
	})
}

// AddTranslationsForNewKeys force-pushes translations for freshly synced source keys.
// Accent creates each new key in every language holding the source text, so a plain diff sees nothing to push.
func AddTranslationsForNewKeys(client *api.Client, file config.File, newKeySet map[string]bool, verbose bool) error {
	return ForEachTranslationFile(file, func(doc Document) error {
		obj, err := ReadJSONObjectFile(doc.LocalPath)
		if err != nil {
			return err
		}
		var nodes []NodeEntry
		for _, n := range CollectNodes(obj, nil) {
			if newKeySet[NodeKey(n.Path)] {
				nodes = append(nodes, n)
			}
		}
		if len(nodes) == 0 {
			output.Info(fmt.Sprintf("%s: no new translations", doc.LocalPath))
			return nil
		}
		return uploadTranslationChunks(client, nodes, doc, "force", verbose)
	})
}

// uploadTranslationChunks sends nodes to /add-translations in disjoint batches:
// a merge only touches keys present in the upload, so chunks need not accumulate.
func uploadTranslationChunks(client *api.Client, nodes []NodeEntry, doc Document, mergeType string, verbose bool) error {
	nChunks := ChunkCount(len(nodes), constants.ChunkSize)
	output.Info(fmt.Sprintf("%s: %d translations -> %d chunk(s)", doc.LocalPath, len(nodes), nChunks))

	opts := api.AddTranslationsOptions{MergeType: mergeType}
	for start := 0; start < len(nodes); start += constants.ChunkSize {
		end := min(start+constants.ChunkSize, len(nodes))
		chunkNum := start/constants.ChunkSize + 1

		err := WithTempNodeFile(doc.LocalPath, nodes[start:end], "accentctl-trans-*.json", func(tmpName string) error {
			if verbose {
				output.Info(fmt.Sprintf("chunk %d/%d: %s", chunkNum, nChunks, tmpName))
			}
			err := client.AddTranslations(tmpName, doc.Path, doc.Format, doc.Language, opts)
			if errors.Is(err, api.ErrNotFound) {
				return err
			}
			if err != nil {
				return fmt.Errorf("%s chunk %d/%d: %w", doc.LocalPath, chunkNum, nChunks, err)
			}
			if verbose {
				output.FileAddTranslations(fmt.Sprintf("%s [chunk %d/%d]", doc.LocalPath, chunkNum, nChunks))
			} else {
				output.ChunkProgress(doc.LocalPath, chunkNum, nChunks)
			}
			return nil
		})
		if errors.Is(err, api.ErrNotFound) {
			output.Warn(fmt.Sprintf("%s: skipped, the Accent project has no %q language (add it in Accent or rename the folder)",
				doc.LocalPath, doc.Language))
			return nil
		}
		if err != nil {
			return err
		}
	}
	return nil
}
