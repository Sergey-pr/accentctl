package cmd

import (
	"errors"
	"fmt"
	"os/exec"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/spf13/cobra"

	"github.com/sergey-pr/accentctl/internal/api"
	"github.com/sergey-pr/accentctl/internal/config"
	"github.com/sergey-pr/accentctl/internal/output"
	"github.com/sergey-pr/accentctl/internal/utils"
)

var pullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Pull files from Accent to your local filesystem",
	Example: `  accentctl pull
  accentctl pull --order-by key`,
	RunE: runPull,
}

var pullOrderBy string

func init() {
	pullCmd.Flags().StringVar(&pullOrderBy, "order-by", "key", "Order of exported keys: index, -index, key, -key, updated, -updated")
}

func runPull(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	client := api.New(cfg.APIURL, cfg.APIKey, verbose)
	output.Section("Pulling files")

	for _, file := range cfg.Files {
		if err := runHooks(file.Hooks.BeforeExport); err != nil {
			return fmt.Errorf("beforeExport hook failed: %w", err)
		}

		if err := pullFile(client, file, pullOrderBy); err != nil {
			return err
		}

		if err := runHooks(file.Hooks.AfterExport); err != nil {
			return fmt.Errorf("afterExport hook failed: %w", err)
		}
	}

	return nil
}

func pullFile(client *api.Client, file config.File, orderBy string) error {
	slugs, err := utils.LanguageSlugsFromFilesystem(file.Target)
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
			}
			first = false

			docPath := utils.DocumentName(src)
			targetPath := utils.ApplyTargetTemplate(file.Target, slug, docPath)
			err := client.Export(targetPath, docPath, file.Format, slug, opts)
			if errors.Is(err, api.ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if file.Format == "json" && (orderBy == "key" || orderBy == "-key") {
				if err := utils.SortJSONFile(targetPath, orderBy == "-key"); err != nil {
					return err
				}
			}
			output.FileExport(targetPath)
		}
	}

	return nil
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
