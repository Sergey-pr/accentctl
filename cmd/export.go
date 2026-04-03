package cmd

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/spf13/cobra"
	"time"

	"github.com/sergey-pr/accentctl/internal/api"
	"github.com/sergey-pr/accentctl/internal/config"
	"github.com/sergey-pr/accentctl/internal/output"
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export files from Accent to your local filesystem",
	Example: `  accentctl export
  accentctl export --order-by key-asc`,
	RunE: runExport,
}

var exportOrderBy string

func init() {
	exportCmd.Flags().StringVar(&exportOrderBy, "order-by", "index", "Order of exported keys: index, -index, key, -key, updated, -updated")
}

func runExport(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	client := api.New(cfg.APIURL, cfg.APIKey, verbose)
	output.Section("Exporting files")

	for _, file := range cfg.Files {
		if err := runHooks(file.Hooks.BeforeExport); err != nil {
			return fmt.Errorf("beforeExport hook failed: %w", err)
		}

		if err := exportFile(client, file, exportOrderBy); err != nil {
			return err
		}

		if err := runHooks(file.Hooks.AfterExport); err != nil {
			return fmt.Errorf("afterExport hook failed: %w", err)
		}
	}

	return nil
}

func exportFile(client *api.Client, file config.File, orderBy string) error {
	slugs, err := languageSlugsFromFilesystem(file.Target)
	if err != nil {
		return err
	}

	sources, err := doublestar.FilepathGlob(file.Source)
	if err != nil {
		return fmt.Errorf("invalid source pattern %q: %w", file.Source, err)
	}
	if len(sources) == 0 {
		return fmt.Errorf("no files matched source pattern %q", file.Source)
	}

	opts := api.ExportOptions{OrderBy: orderBy}

	first := true
	for _, slug := range slugs {
		for _, src := range sources {
			if !first {
				time.Sleep(time.Second)
			}
			first = false

			docPath := documentName(src)
			targetPath := applyTargetTemplate(file.Target, slug, docPath)
			err := client.Export(targetPath, docPath, file.Format, slug, opts)
			if errors.Is(err, api.ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if file.Format == "json" && (orderBy == "key" || orderBy == "-key") {
				if err := sortJSONFile(targetPath, orderBy == "-key"); err != nil {
					return err
				}
			}
			output.FileExport(targetPath)
		}
	}

	return nil
}

// documentName strips directory and extension.
func documentName(filePath string) string {
	base := filepath.Base(filePath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// applyTargetTemplate replaces placeholders in the target pattern.
// docPath is the document name without extension (e.g. "en", "common").
// Supported placeholders:
//
//	%slug%               — language slug (e.g. "fr")
//	%document_path%      — document name without extension (e.g. "common")
//	%original_file_name% — document name with .json extension (e.g. "common.json")
func applyTargetTemplate(target, language, docPath string) string {
	r := strings.NewReplacer(
		"%slug%", language,
		"%document_path%", docPath,
		"%original_file_name%", docPath+".json",
	)
	return r.Replace(target)
}

// languageFromPath extracts the language slug from a file path by matching it
// against the target template. For example, source "locales/en/foo.json" with
// target "locales/%slug%/%document_path%.json" returns "en".
// Returns empty string if the slug cannot be determined.
func languageFromPath(filePath, target string) string {
	// Escape regex metacharacters in the template, then replace placeholders
	// with capture/wildcard groups.
	pattern := regexp.QuoteMeta(target)
	pattern = strings.ReplaceAll(pattern, regexp.QuoteMeta("%slug%"), "([^/]+)")
	pattern = strings.ReplaceAll(pattern, regexp.QuoteMeta("%document_path%"), "[^/]+")
	pattern = strings.ReplaceAll(pattern, regexp.QuoteMeta("%original_file_name%"), "[^/]+")

	re, err := regexp.Compile("^" + pattern + "$")
	if err != nil {
		return ""
	}

	// Normalise path separators on Windows
	normalised := filepath.ToSlash(filePath)
	matches := re.FindStringSubmatch(normalised)
	if len(matches) < 2 {
		return ""
	}
	return matches[1]
}

// languageSlugsFromFilesystem discovers language slugs by globbing the target
// template with %slug% replaced by * and extracting the slug from each match.
// Works for both "locales/%slug%.json" and "locales/%slug%/file.json" patterns.
func languageSlugsFromFilesystem(target string) ([]string, error) {
	if !strings.Contains(target, "%slug%") {
		return nil, fmt.Errorf("target %q does not contain %%slug%%", target)
	}
	pattern := strings.ReplaceAll(target, "%slug%", "*")
	pattern = strings.ReplaceAll(pattern, "%document_path%", "*")
	pattern = strings.ReplaceAll(pattern, "%original_file_name%", "*")

	matches, err := doublestar.FilepathGlob(pattern)
	if err != nil {
		return nil, fmt.Errorf("could not glob %q: %w", pattern, err)
	}

	seen := map[string]bool{}
	var slugs []string
	for _, match := range matches {
		slug := languageFromPath(filepath.ToSlash(match), target)
		if slug != "" && !seen[slug] {
			seen[slug] = true
			slugs = append(slugs, slug)
		}
	}
	sort.Strings(slugs)
	return slugs, nil
}

func runHooks(hooks []string) error {
	for _, h := range hooks {
		output.Hook(h)
		c := exec.Command("sh", "-c", h)
		c.Stdout = nil
		c.Stderr = nil
		if err := c.Run(); err != nil {
			return fmt.Errorf("hook %q: %w", h, err)
		}
	}
	return nil
}
