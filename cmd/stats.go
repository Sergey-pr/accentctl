package cmd

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/sergey-pr/accentctl/internal/api"
	"github.com/sergey-pr/accentctl/internal/config"
	"github.com/sergey-pr/accentctl/internal/output"
)

var statsCmd = &cobra.Command{
	Use:     "stats",
	Short:   "Show translation statistics for the project",
	Example: `  accentctl stats`,
	RunE:    runStats,
}

func runStats(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	client := api.New(cfg.APIURL, cfg.APIKey)
	stats, err := client.Stats()
	if err != nil {
		return err
	}

	output.Section(fmt.Sprintf("Project: %s", stats.Project.Name))
	fmt.Printf("  Translated:  %.1f%%\n", stats.Project.TranslatedRate*100)
	fmt.Printf("  Documents:   %d\n", stats.Project.DocumentsCount)
	fmt.Printf("  Versions:    %d\n", stats.Project.VersionsCount)

	if len(stats.LanguageStats) > 0 {
		output.Section("Languages")
		bold := color.New(color.Bold)
		for _, ls := range stats.LanguageStats {
			pct := ls.TranslatedRate * 100
			bar := progressBar(pct, 20)
			bold.Printf("  %-12s", ls.Language.Slug)
			fmt.Printf(" %s %5.1f%%  (%d translated, %d missing)\n",
				bar, pct, ls.TranslatedCount, ls.UntranslatedCount)
		}
	}

	return nil
}

func progressBar(pct float64, width int) string {
	filled := int(pct / 100 * float64(width))
	bar := ""
	for i := 0; i < width; i++ {
		if i < filled {
			bar += "█"
		} else {
			bar += "░"
		}
	}
	return bar
}
