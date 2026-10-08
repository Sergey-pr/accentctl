package cmd

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/sergey-pr/accentctl/internal/config"
)

func runInitWithInput(t *testing.T, input string) error {
	t.Helper()
	t.Chdir(t.TempDir())
	t.Setenv("ACCENT_API_KEY", "key")
	t.Setenv("ACCENT_API_URL", "")

	f, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(input); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	stdin := os.Stdin
	os.Stdin = f
	t.Cleanup(func() {
		os.Stdin = stdin
		_ = f.Close()
	})
	return runInit(nil, nil)
}

func TestInitAsksAgainForTargetWithoutSlug(t *testing.T) {
	defaults := strings.Repeat("\n", 5)
	if err := runInitWithInput(t, defaults+"localization/fr/%original_file_name%\n\n"); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile("accent.json")
	if err != nil {
		t.Fatal(err)
	}
	var written struct {
		Files []struct {
			Target string `json:"target"`
		} `json:"files"`
	}
	if err := json.Unmarshal(raw, &written); err != nil {
		t.Fatal(err)
	}
	if got := written.Files[0].Target; got != "localization/%slug%/%original_file_name%" {
		t.Errorf("target = %q, want the default given after the rejected one", got)
	}
	if _, err := config.Load(); err != nil {
		t.Errorf("config.Load rejected the config init wrote: %v", err)
	}
}

func TestInitWritesNothingWhenInputEndsAfterInvalidTarget(t *testing.T) {
	defaults := strings.Repeat("\n", 5)
	if err := runInitWithInput(t, defaults+"localization/fr/%original_file_name%\n"); err == nil {
		t.Error("init succeeded with only an invalid target, want an error")
	}
	if _, err := os.Stat("accent.json"); !os.IsNotExist(err) {
		t.Error("init wrote accent.json without a valid target")
	}
}
