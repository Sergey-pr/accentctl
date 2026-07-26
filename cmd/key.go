package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var keyCmd = &cobra.Command{
	Use:   "key",
	Short: "Manage your local API key",
}

var keySetCmd = &cobra.Command{
	Use:   "set [apikey]",
	Short: "Save an API key to accent.local.json (gitignored)",
	Args:  cobra.MaximumNArgs(1),
	Example: `  accentctl key set --stdin < key.txt
  echo your-api-key | accentctl key set --stdin
  accentctl key set your-api-key`,
	RunE: runKeySet,
}

var keyStdin bool

func init() {
	keySetCmd.Flags().BoolVar(&keyStdin, "stdin", false, "Read the API key from stdin instead of an argument")
	keyCmd.AddCommand(keySetCmd)
}

func runKeySet(_ *cobra.Command, args []string) error {
	key, err := readAPIKey(args, os.Stdin)
	if err != nil {
		return err
	}
	return saveLocalAPIKey(key)
}

// readAPIKey takes the key from stdin or the argument. Passing it as an argument
// leaves the secret in shell history, so --stdin is the documented path.
func readAPIKey(args []string, stdin io.Reader) (string, error) {
	if keyStdin {
		if len(args) > 0 {
			return "", fmt.Errorf("pass the key either as an argument or via --stdin, not both")
		}
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("reading API key from stdin: %w", err)
		}
		key := strings.TrimSpace(string(data))
		if key == "" {
			return "", fmt.Errorf("no API key on stdin")
		}
		return key, nil
	}

	if len(args) == 0 {
		return "", fmt.Errorf("provide the API key as an argument or use --stdin")
	}
	if key := strings.TrimSpace(args[0]); key != "" {
		return key, nil
	}
	return "", fmt.Errorf("the API key is empty")
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
