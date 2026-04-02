package cmd

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

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
	exportCmd.Flags().StringVar(&exportOrderBy, "order-by", "index", "Order of exported keys: index or key-asc")
}

func runExport(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	client := api.New(cfg.APIURL, cfg.APIKey)
	output.Section("Exporting files")

	for _, file := range cfg.Files {
		if err := runHooks(file.Hooks.BeforeExport); err != nil {
			return fmt.Errorf("beforeExport hook failed: %w", err)
		}

		if err := exportFile(client, file); err != nil {
			return err
		}

		if err := runHooks(file.Hooks.AfterExport); err != nil {
			return fmt.Errorf("afterExport hook failed: %w", err)
		}
	}

	return nil
}

func exportFile(client *api.Client, file config.File) error {
	// Resolve source glob to find existing source files and infer document paths
	sources, err := doublestar.FilepathGlob(file.Source)
	if err != nil {
		return fmt.Errorf("invalid source pattern %q: %w", file.Source, err)
	}

	// We need one export per language per document. Since Accent holds
	// translations for all languages, we derive target paths from the
	// target template (which contains %slug% and %original_file_name%).
	if len(sources) == 0 {
		return fmt.Errorf("no files matched source pattern %q", file.Source)
	}

	opts := api.ExportOptions{OrderBy: exportOrderBy}

	var g errgroup.Group
	for _, src := range sources {
		src := src
		documentPath := documentName(src)

		// Build the target path for the source language (direct replacement)
		targetPath := applyTargetTemplate(file.Target, file.Language, src)

		g.Go(func() error {
			output.FileExport(targetPath)
			return client.Export(targetPath, documentPath, file.Format, file.Language, opts)
		})
	}

	return g.Wait()
}

// documentName strips directory and extension.
func documentName(filePath string) string {
	base := filepath.Base(filePath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// applyTargetTemplate replaces %slug% and %original_file_name% in the target pattern.
func applyTargetTemplate(target, language, sourcePath string) string {
	name := documentName(sourcePath)
	r := strings.NewReplacer(
		"%slug%", language,
		"%original_file_name%", name+filepath.Ext(sourcePath),
	)
	return r.Replace(target)
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
