package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeConfig chdirs into a fresh temp dir holding an accent.json with the given
// top-level extras merged in.
func writeConfig(t *testing.T, extra string) {
	t.Helper()
	t.Chdir(t.TempDir())
	t.Setenv("ACCENT_API_KEY", "")
	t.Setenv("ACCENT_API_URL", "")

	cfg := `{
  "apiUrl": "https://accent.test",
  "apiKey": "key",` + extra + `
  "files": [{"source": "localization/en/*.json", "target": "localization/%slug%/%original_file_name%"}]
}`
	if err := os.WriteFile("accent.json", []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadFindsConfigFromSubdirectory(t *testing.T) {
	writeConfig(t, "")
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll("localization/en", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("localization/en/app.json", []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	sub := filepath.Join(root, "deep", "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load from subdirectory: %v", err)
	}
	sources, err := cfg.Files[0].Sources()
	if err != nil {
		t.Fatalf("Sources after subdirectory load: %v", err)
	}
	if len(sources) != 1 {
		t.Errorf("resolved %d sources, want 1 (globs must resolve from the project root)", len(sources))
	}
}

func TestLoadMissingConfigStillErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("ACCENT_API_KEY", "")
	t.Setenv("ACCENT_API_URL", "")

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "could not read config file") {
		t.Errorf("err = %v, want the not-found message", err)
	}
}

func TestRequestDelay(t *testing.T) {
	tests := []struct {
		name    string
		extra   string
		env     string
		want    time.Duration
		wantErr string
	}{
		{name: "defaults to 1.5s", want: 1500 * time.Millisecond},
		{name: "config value", extra: `"requestDelay": "500ms",`, want: 500 * time.Millisecond},
		{name: "zero disables throttling", extra: `"requestDelay": "0s",`, want: 0},
		{name: "env overrides config", extra: `"requestDelay": "5s",`, env: "250ms", want: 250 * time.Millisecond},
		{name: "env overrides default", env: "0s", want: 0},
		{name: "invalid env", env: "soon", wantErr: "invalid ACCENT_REQUEST_DELAY"},
		{name: "bare number rejected", extra: `"requestDelay": 500,`, wantErr: "quoted duration string"},
		{name: "negative rejected", extra: `"requestDelay": "-1s",`, wantErr: "cannot be negative"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writeConfig(t, tt.extra)
			if tt.env != "" {
				t.Setenv("ACCENT_REQUEST_DELAY", tt.env)
			}

			cfg, err := Load()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to mention %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.RequestDelay != tt.want {
				t.Errorf("RequestDelay = %s, want %s", cfg.RequestDelay, tt.want)
			}
		})
	}
}
