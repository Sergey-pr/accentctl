package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var keyCmd = &cobra.Command{
	Use:   "key",
	Short: "Manage your local API key",
}

var keySetCmd = &cobra.Command{
	Use:     "set <apikey>",
	Short:   "Save an API key to accent.local.json (gitignored)",
	Args:    cobra.ExactArgs(1),
	Example: `  accentctl key set your-api-key`,
	RunE:    runKeySet,
}

func init() {
	keyCmd.AddCommand(keySetCmd)
}

func runKeySet(_ *cobra.Command, args []string) error {
	return saveLocalAPIKey(args[0])
}

func saveLocalAPIKey(apiKey string) error {
	const localFile = "accent.local.json"

	// Read existing file to preserve any other fields.
	data := map[string]any{}
	if raw, err := os.ReadFile(localFile); err == nil {
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("%s is not valid JSON: %w", localFile, err)
		}
	}

	data["apiKey"] = apiKey

	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(localFile, append(out, '\n'), 0o600); err != nil {
		return err
	}

	fmt.Printf("API key saved to %s\n", localFile)
	return nil
}
