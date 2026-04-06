package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:     "init",
	Short:   "Create a starter accent.json config file",
	Example: `  accentctl init`,
	RunE:    runInit,
}

func runInit(_ *cobra.Command, _ []string) error {
	const configFile = "accent.json"

	if _, err := os.Stat(configFile); err == nil {
		return fmt.Errorf("%s already exists", configFile)
	}

	r := bufio.NewReader(os.Stdin)

	apiURL, err := prompt(r, "Accent API URL", "https://your.accent.instance")
	if err != nil {
		return err
	}
	apiKey, err := prompt(r, "API key (or leave blank to use ACCENT_API_KEY env var)", "")
	if err != nil {
		return err
	}
	language, err := prompt(r, "Source language slug", "en")
	if err != nil {
		return err
	}
	format, err := prompt(r, "File format", "json")
	if err != nil {
		return err
	}
	source, err := prompt(r, "Source file", "localization/en/*.json")
	if err != nil {
		return err
	}
	target, err := prompt(r, "Target path template", "localization/%slug%/%original_file_name%")
	if err != nil {
		return err
	}

	cfg := map[string]any{
		"apiUrl": apiURL,
		"files": []map[string]any{
			{
				"language": language,
				"format":   format,
				"source":   source,
				"target":   target,
			},
		},
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(configFile, append(data, '\n'), 0o644); err != nil {
		return err
	}

	fmt.Printf("\nCreated %s\n", configFile)

	if apiKey != "" {
		if err := saveLocalAPIKey(apiKey); err != nil {
			return err
		}
	} else {
		fmt.Println("Remember to set your API key via `accentctl key set <apikey>` or the ACCENT_API_KEY environment variable.")
	}
	return nil
}

func prompt(r *bufio.Reader, label, defaultVal string) (string, error) {
	if defaultVal != "" {
		fmt.Printf("%s [%s]: ", label, defaultVal)
	} else {
		fmt.Printf("%s: ", label)
	}
	line, err := r.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading input for %q: %w", label, err)
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultVal, nil
	}
	return line, nil
}
