package cmd

import (
	"fmt"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/s-mage/accentctl/internal/api"
	"github.com/s-mage/accentctl/internal/config"
	"github.com/s-mage/accentctl/internal/output"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync local files with Accent",
	Long: `Upload local source files to Accent. By default runs a dry-run (peek)
showing what would change. Use --write to apply changes and download
the updated exports.`,
	Example: `  accentctl sync
  accentctl sync --write
  accentctl sync --write --add-translations --merge-type force`,
	RunE: runSync,
}

var (
	syncWrite           bool
	syncAddTranslations bool
	syncType            string
	syncMergeType       string
	syncOrderBy         string
)

func init() {
	syncCmd.Flags().BoolVar(&syncWrite, "write", false, "Apply changes and write exported files locally")
	syncCmd.Flags().BoolVar(&syncAddTranslations, "add-translations", false, "Upload existing translations to Accent")
	syncCmd.Flags().StringVar(&syncType, "sync-type", "smart", "Sync strategy: smart or passive")
	syncCmd.Flags().StringVar(&syncMergeType, "merge-type", "smart", "Merge strategy for add-translations: smart, passive, or force")
	syncCmd.Flags().StringVar(&syncOrderBy, "order-by", "index", "Order of exported keys: index or key-asc")
}

func runSync(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	client := api.New(cfg.APIURL, cfg.APIKey)
	output.Section("Syncing files")

	for _, file := range cfg.Files {
		if err := runHooks(file.Hooks.BeforeSync); err != nil {
			return fmt.Errorf("beforeSync hook failed: %w", err)
		}

		if err := syncFile(client, file); err != nil {
			return err
		}

		if err := runHooks(file.Hooks.AfterSync); err != nil {
			return fmt.Errorf("afterSync hook failed: %w", err)
		}
	}

	if syncAddTranslations {
		output.Section("Adding translations")
		for _, file := range cfg.Files {
			if err := runHooks(file.Hooks.BeforeAddTranslations); err != nil {
				return fmt.Errorf("beforeAddTranslations hook failed: %w", err)
			}
			if err := addTranslationsFile(client, file); err != nil {
				return err
			}
			if err := runHooks(file.Hooks.AfterAddTranslations); err != nil {
				return fmt.Errorf("afterAddTranslations hook failed: %w", err)
			}
		}
	}

	if syncWrite {
		output.Section("Exporting updated files")
		for _, file := range cfg.Files {
			if err := exportFile(client, file); err != nil {
				return err
			}
		}
	}

	return nil
}

func syncFile(client *api.Client, file config.File) error {
	sources, err := doublestar.FilepathGlob(file.Source)
	if err != nil {
		return fmt.Errorf("invalid source pattern %q: %w", file.Source, err)
	}
	if len(sources) == 0 {
		return fmt.Errorf("no files matched source pattern %q", file.Source)
	}

	opts := api.SyncOptions{
		DryRun:   !syncWrite,
		SyncType: syncType,
		OrderBy:  syncOrderBy,
	}

	var g errgroup.Group
	for _, src := range sources {
		src := src
		g.Go(func() error {
			documentPath := documentName(src)
			peek, err := client.Sync(src, documentPath, file.Format, file.Language, opts)
			if err != nil {
				return fmt.Errorf("%s: %w", src, err)
			}
			if peek != nil {
				output.FileDryRun(src, peek.NewCount, peek.UpdatedCount, peek.RemovedCount)
			} else {
				output.FileSync(src)
			}
			return nil
		})
	}

	return g.Wait()
}

func addTranslationsFile(client *api.Client, file config.File) error {
	sources, err := doublestar.FilepathGlob(file.Source)
	if err != nil {
		return fmt.Errorf("invalid source pattern %q: %w", file.Source, err)
	}

	opts := api.AddTranslationsOptions{
		DryRun:    !syncWrite,
		MergeType: syncMergeType,
	}

	var g errgroup.Group
	for _, src := range sources {
		src := src
		g.Go(func() error {
			documentPath := documentName(src)
			peek, err := client.AddTranslations(src, documentPath, file.Format, file.Language, opts)
			if err != nil {
				return fmt.Errorf("%s: %w", src, err)
			}
			if peek != nil {
				output.FileDryRun(src, peek.NewCount, peek.UpdatedCount, peek.RemovedCount)
			} else {
				output.FileAddTranslations(src)
			}
			return nil
		})
	}

	return g.Wait()
}
