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

// forEachTranslationFile calls fn for every target-language file that exists
// locally, skipping the source language.
func forEachTranslationFile(file config.File, fn func(localPath, docPath, slug string) error) error {
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
			if err := fn(localPath, DocumentName(src), slug); err != nil {
				return err
			}
		}
	}
	return nil
}

// AddAllTranslations pushes every local translation without diffing against the
// server. A "passive" merge keeps reviewer-corrected strings, so recovery cannot clobber them.
func AddAllTranslations(client *api.Client, file config.File, mergeType string, verbose bool) error {
	return forEachTranslationFile(file, func(localPath, docPath, slug string) error {
		obj, err := ReadJSONObjectFile(localPath)
		if err != nil {
			return err
		}
		nodes := CollectNodes(obj, nil)
		if len(nodes) == 0 {
			output.Info(fmt.Sprintf("%s: no translations", localPath))
			return nil
		}
		return uploadTranslationChunks(client, nodes, localPath, docPath, file.Format, slug, mergeType, verbose)
	})
}

// AddTranslationsForNewKeys force-pushes translations for freshly synced source keys.
// Accent creates each new key in every language holding the source text, so a plain diff sees nothing to push.
func AddTranslationsForNewKeys(client *api.Client, file config.File, newKeySet map[string]bool, verbose bool) error {
	return forEachTranslationFile(file, func(localPath, docPath, slug string) error {
		obj, err := ReadJSONObjectFile(localPath)
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
			output.Info(fmt.Sprintf("%s: no new translations", localPath))
			return nil
		}
		return uploadTranslationChunks(client, nodes, localPath, docPath, file.Format, slug, "force", verbose)
	})
}

// uploadTranslationChunks sends nodes to /add-translations in disjoint batches:
// a merge only touches keys present in the upload, so chunks need not accumulate.
func uploadTranslationChunks(client *api.Client, nodes []NodeEntry, localPath, docPath, format, slug, mergeType string, verbose bool) error {
	nChunks := (len(nodes) + constants.ChunkSize - 1) / constants.ChunkSize
	output.Info(fmt.Sprintf("%s: %d translations -> %d chunk(s)", localPath, len(nodes), nChunks))

	opts := api.AddTranslationsOptions{MergeType: mergeType}
	for start := 0; start < len(nodes); start += constants.ChunkSize {
		end := min(start+constants.ChunkSize, len(nodes))
		chunkNum := start/constants.ChunkSize + 1

		tmpName, err := WriteNodesTempFile(nodes[start:end], "accentctl-trans-*.json")
		if err != nil {
			return err
		}
		if verbose {
			output.Info(fmt.Sprintf("chunk %d/%d: %s", chunkNum, nChunks, tmpName))
		}
		err = client.AddTranslations(tmpName, docPath, format, slug, opts)
		_ = os.Remove(tmpName)
		if errors.Is(err, api.ErrNotFound) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%s chunk %d/%d: %w", localPath, chunkNum, nChunks, err)
		}
		if verbose {
			output.FileAddTranslations(fmt.Sprintf("%s [chunk %d/%d]", localPath, chunkNum, nChunks))
		} else {
			output.ChunkProgress(localPath, chunkNum, nChunks)
		}
	}
	return nil
}
