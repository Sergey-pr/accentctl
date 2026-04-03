package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

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

	slugs, err := languageSlugsFromFilesystem(cfg.Files[0].Target)
	if err != nil {
		return err
	}

	output.Section("Languages")
	for _, slug := range slugs {
		fmt.Printf("  %s\n", slug)
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
