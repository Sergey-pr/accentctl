package cmd

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestReadAPIKey(t *testing.T) {
	tests := []struct {
		name    string
		stdin   bool
		args    []string
		input   string
		want    string
		wantErr string
	}{
		{name: "argument", args: []string{"secret"}, want: "secret"},
		{name: "argument is trimmed", args: []string{"  secret\n"}, want: "secret"},
		{name: "stdin", stdin: true, input: "secret\n", want: "secret"},
		{name: "stdin without trailing newline", stdin: true, input: "secret", want: "secret"},
		{name: "no argument", wantErr: "provide the API key"},
		{name: "empty argument", args: []string{"   "}, wantErr: "empty"},
		{name: "empty stdin", stdin: true, input: "\n", wantErr: "no API key on stdin"},
		{name: "both sources", stdin: true, args: []string{"secret"}, input: "other", wantErr: "not both"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keyStdin = tt.stdin
			t.Cleanup(func() { keyStdin = false })

			got, err := readAPIKey(tt.args, strings.NewReader(tt.input))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to mention %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("key = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSaveLocalAPIKeyPreservesOtherFields(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("accent.local.json", []byte(`{"apiUrl":"https://kept.test","apiKey":"old"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := saveLocalAPIKey("new"); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile("accent.local.json")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["apiKey"] != "new" {
		t.Errorf("apiKey = %v, want %q", got["apiKey"], "new")
	}
	if got["apiUrl"] != "https://kept.test" {
		t.Errorf("apiUrl = %v, want it preserved", got["apiUrl"])
	}
}

// The key file holds a secret and must not be world-readable.
func TestSaveLocalAPIKeyFileMode(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := saveLocalAPIKey("secret"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat("accent.local.json")
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("file mode = %o, want 600", got)
	}
}
