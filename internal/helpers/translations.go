package helpers

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sergey-pr/accentctl/internal/api"
	"github.com/sergey-pr/accentctl/internal/config"
	"github.com/sergey-pr/accentctl/internal/constants"
	"github.com/sergey-pr/accentctl/internal/output"
)

func AddTranslationsFile(client *api.Client, file config.File, mergeType string, verbose bool) error {
	slugs, err := LanguageSlugsFromFilesystem(file.Target)
	if err != nil {
		return err
	}

	sources, err := file.Sources()
	if err != nil {
		return err
	}

	// Derive source language from the first source file if not set in config.
	sourceLanguage := file.Language
	if sourceLanguage == "" && len(sources) > 0 {
		sourceLanguage = LanguageFromPath(filepath.ToSlash(sources[0]), file.Target)
	}

	for _, slug := range slugs {
		if slug == sourceLanguage {
			continue
		}
		for _, src := range sources {
			docPath := DocumentName(src)
			localPath := ApplyTargetTemplate(file.Target, slug, docPath)
			if _, err := os.Stat(localPath); err != nil {
				continue
			}

			if err := addTranslationsChunked(client, localPath, docPath, file.Format, slug, mergeType, verbose); err != nil {
				return err
			}
		}
	}

	return nil
}

// AddAllTranslations uploads every translation in each target language's local
// file with the given merge type, without diffing against the server.
//
// Recovery from an interrupted sync needs this: the source keys already exist
// on the server, so the key-presence diff the other paths use (NewKeysChunks)
// finds nothing to push. Uploading everything and letting the server decide
// per-key is only safe with mergeType "passive", which skips any string a
// reviewer has corrected in Accent and cannot create or remove keys.
func AddAllTranslations(client *api.Client, file config.File, mergeType string, verbose bool) error {
	slugs, err := LanguageSlugsFromFilesystem(file.Target)
	if err != nil {
		return err
	}
	sources, err := file.Sources()
	if err != nil {
		return err
	}
	sourceLanguage := file.Language
	if sourceLanguage == "" && len(sources) > 0 {
		sourceLanguage = LanguageFromPath(filepath.ToSlash(sources[0]), file.Target)
	}

	for _, slug := range slugs {
		if slug == sourceLanguage {
			continue
		}
		for _, src := range sources {
			docPath := DocumentName(src)
			localPath := ApplyTargetTemplate(file.Target, slug, docPath)
			if _, err := os.Stat(localPath); err != nil {
				continue
			}

			data, err := os.ReadFile(localPath)
			if err != nil {
				return fmt.Errorf("%s: %w", localPath, err)
			}
			obj, err := ParseJSONObject(data)
			if err != nil || obj == nil {
				continue
			}
			nodes := CollectNodes(obj, nil)
			if len(nodes) == 0 {
				output.Info(fmt.Sprintf("%s: no translations", localPath))
				continue
			}

			if err := uploadTranslationChunks(client, nodes, localPath, docPath, file.Format, slug, mergeType, verbose); err != nil {
				return err
			}
		}
	}
	return nil
}

// uploadTranslationChunks uploads nodes to /add-translations in batches of
// ChunkSize. The chunks are disjoint rather than cumulative: a merge only ever
// touches keys present in the uploaded file, so there is no need to re-send
// earlier keys to keep them alive.
func uploadTranslationChunks(client *api.Client, nodes []NodeEntry, localPath, docPath, format, slug, mergeType string, verbose bool) error {
	nChunks := (len(nodes) + constants.ChunkSize - 1) / constants.ChunkSize
	output.Info(fmt.Sprintf("%s: %d translations -> %d chunk(s)", localPath, len(nodes), nChunks))

	opts := api.AddTranslationsOptions{MergeType: mergeType}
	for i := 0; i < len(nodes); i += constants.ChunkSize {
		end := i + constants.ChunkSize
		if end > len(nodes) {
			end = len(nodes)
		}
		chunkNum := i/constants.ChunkSize + 1

		chunkData, err := MarshalNodes(nodes[i:end])
		if err != nil {
			return err
		}
		tmp, err := os.CreateTemp("", "accentctl-trans-*.json")
		if err != nil {
			return err
		}
		if _, err := tmp.Write(chunkData); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
			return err
		}
		_ = tmp.Close()

		if verbose {
			output.Info(fmt.Sprintf("chunk %d/%d: %s", chunkNum, nChunks, tmp.Name()))
		}
		_, err = client.AddTranslations(tmp.Name(), docPath, format, slug, opts)
		_ = os.Remove(tmp.Name())
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

// AddTranslationsForNewKeys force-pushes translations for keys that were just
// added to the source language. Accent creates a new source key in every
// language at once, holding the source text, so a normal new-key diff finds
// nothing: these keys must be targeted explicitly using a force merge.
func AddTranslationsForNewKeys(client *api.Client, file config.File, newKeySet map[string]bool, verbose bool) error {
	slugs, err := LanguageSlugsFromFilesystem(file.Target)
	if err != nil {
		return err
	}
	sources, err := file.Sources()
	if err != nil {
		return err
	}
	sourceLanguage := file.Language
	if sourceLanguage == "" && len(sources) > 0 {
		sourceLanguage = LanguageFromPath(filepath.ToSlash(sources[0]), file.Target)
	}

	for _, slug := range slugs {
		if slug == sourceLanguage {
			continue
		}
		for _, src := range sources {
			docPath := DocumentName(src)
			localPath := ApplyTargetTemplate(file.Target, slug, docPath)
			if _, err := os.Stat(localPath); err != nil {
				continue
			}

			data, err := os.ReadFile(localPath)
			if err != nil {
				return fmt.Errorf("%s: %w", localPath, err)
			}
			obj, err := ParseJSONObject(data)
			if err != nil || obj == nil {
				continue
			}

			// Keep only nodes whose path is in the new-key set.
			var targetNodes []NodeEntry
			for _, l := range CollectNodes(obj, nil) {
				if newKeySet[NodeKey(l.Path)] {
					targetNodes = append(targetNodes, l)
				}
			}
			if len(targetNodes) == 0 {
				output.Info(fmt.Sprintf("%s: no new translations", localPath))
				continue
			}

			if err := uploadTranslationChunks(client, targetNodes, localPath, docPath, file.Format, slug, "force", verbose); err != nil {
				return err
			}
		}
	}
	return nil
}

func addTranslationsChunked(client *api.Client, localPath, docPath, format, language, mergeType string, verbose bool) error {
	var existing []byte
	if mergeType != "force" {
		var err error
		existing, err = client.ExportBytes(docPath, format, language)
		if err != nil {
			return fmt.Errorf("%s: could not fetch existing translations: %w", localPath, err)
		}
	}

	chunks, newCount, err := NewKeysChunks(localPath, existing, constants.ChunkSize)
	if err != nil {
		return fmt.Errorf("%s: chunking failed: %w", localPath, err)
	}

	defer func() {
		for _, p := range chunks {
			if p != localPath {
				_ = os.Remove(p)
			}
		}
	}()

	if len(chunks) == 0 {
		output.Info(fmt.Sprintf("%s: no new translations", localPath))
		return nil
	}

	output.Info(fmt.Sprintf("%s: %d new translations -> %d chunk(s)", localPath, newCount, len(chunks)))

	opts := api.AddTranslationsOptions{MergeType: mergeType}
	for i, chunk := range chunks {
		if verbose {
			output.Info(fmt.Sprintf("chunk %d/%d: %s", i+1, len(chunks), chunk))
		}
		_, err := client.AddTranslations(chunk, docPath, format, language, opts)
		if errors.Is(err, api.ErrNotFound) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%s chunk %d/%d: %w", localPath, i+1, len(chunks), err)
		}
		if verbose {
			output.FileAddTranslations(fmt.Sprintf("%s [chunk %d/%d]", localPath, i+1, len(chunks)))
		} else {
			output.ChunkProgress(localPath, i+1, len(chunks))
		}
	}
	return nil
}
