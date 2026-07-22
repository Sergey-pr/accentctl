package config

import (
	"os"
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
