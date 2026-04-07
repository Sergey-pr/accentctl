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

// AddTranslationsForNewKeys force-pushes translations for keys that were just
// added to the source language. Accent auto-creates empty entries for new
// source keys in all languages, so a normal new-key diff finds nothing we
// must target these keys explicitly using force merge.
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

			// Keep only leaves whose path is in the new-key set.
			var targetLeaves []LeafEntry
			for _, l := range CollectLeaves(obj, nil) {
				if newKeySet[LeafKey(l.Path)] {
					targetLeaves = append(targetLeaves, l)
				}
			}
			if len(targetLeaves) == 0 {
				output.Info(fmt.Sprintf("%s: no new translations", localPath))
				continue
			}

			nChunks := (len(targetLeaves) + constants.ChunkSize - 1) / constants.ChunkSize
			output.Info(fmt.Sprintf("%s: %d new translations -> %d chunk(s)", localPath, len(targetLeaves), nChunks))

			opts := api.AddTranslationsOptions{MergeType: "force"}
			var tmpFiles []string
			for i := 0; i < len(targetLeaves); i += constants.ChunkSize {
				end := i + constants.ChunkSize
				if end > len(targetLeaves) {
					end = len(targetLeaves)
				}
				// Cumulative so earlier chunks aren't absent from later ones.
				chunkData, err := MarshalLeaves(targetLeaves[:end])
				if err != nil {
					return err
				}
				tmp, err := os.CreateTemp("", "accentctl-trans-*.json")
				if err != nil {
					return err
				}
				if _, err := tmp.Write(chunkData); err != nil {
					_ = tmp.Close()
					return err
				}
				_ = tmp.Close()
				tmpFiles = append(tmpFiles, tmp.Name())

				chunkNum := i/constants.ChunkSize + 1
				if verbose {
					output.Info(fmt.Sprintf("chunk %d/%d: %s", chunkNum, nChunks, tmp.Name()))
				}
				_, err = client.AddTranslations(tmp.Name(), docPath, file.Format, slug, opts)
				if errors.Is(err, api.ErrNotFound) {
					break
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
			for _, p := range tmpFiles {
				_ = os.Remove(p)
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
	// With force: pass nil so all local keys are treated as new and uploaded.

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
