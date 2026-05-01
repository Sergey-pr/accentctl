package helpers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDocumentName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"locales/en.json", "en"},
		{"locales/en.US.json", "en.US"},
		{"path/to/common.json", "common"},
		{"file.json", "file"},
		{"noext", "noext"},
		{"dir/sub/deep.json", "deep"},
	}
	for _, tt := range tests {
		if got := DocumentName(tt.input); got != tt.want {
			t.Errorf("DocumentName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestApplyTargetTemplate(t *testing.T) {
	tests := []struct {
		target   string
		language string
		docPath  string
		want     string
	}{
		{
			"locales/%slug%/%original_file_name%",
			"fr", "common",
			"locales/fr/common.json",
		},
		{
			"locales/%slug%/%document_path%",
			"de", "common",
			"locales/de/common",
		},
		{
			"%slug%/translations.json",
			"es", "translations",
			"es/translations.json",
		},
		{
			"locales/%slug%/%original_file_name%",
			"en", "messages",
			"locales/en/messages.json",
		},
	}
	for _, tt := range tests {
		got := ApplyTargetTemplate(tt.target, tt.language, tt.docPath)
		if got != tt.want {
			t.Errorf("ApplyTargetTemplate(%q, %q, %q) = %q, want %q",
				tt.target, tt.language, tt.docPath, got, tt.want)
		}
	}
}

func TestLanguageFromPath(t *testing.T) {
	target := "locales/%slug%/%original_file_name%"
	tests := []struct {
		path string
		want string
	}{
		{"locales/fr/common.json", "fr"},
		{"locales/de/common.json", "de"},
		{"locales/en-US/common.json", "en-US"},
		{"locales/zh_CN/common.json", "zh_CN"},
	}
	for _, tt := range tests {
		if got := LanguageFromPath(tt.path, target); got != tt.want {
			t.Errorf("LanguageFromPath(%q, %q) = %q, want %q", tt.path, target, got, tt.want)
		}
	}
}

func TestLanguageFromPath_noMatch(t *testing.T) {
	target := "locales/%slug%/%original_file_name%"
	if got := LanguageFromPath("completely/different/path.json", target); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestLanguageFromPath_documentPathPlaceholder(t *testing.T) {
	target := "locales/%slug%/%document_path%.json"
	if got := LanguageFromPath("locales/fr/common.json", target); got != "fr" {
		t.Errorf("got %q, want %q", got, "fr")
	}
}

// --- LanguageSlugsFromFilesystem ---

func TestLanguageSlugsFromFilesystem(t *testing.T) {
	// Build a temp dir tree:
	//   locales/en/common.json
	//   locales/fr/common.json
	//   locales/de/common.json
	dir := t.TempDir()
	for _, lang := range []string{"en", "fr", "de"} {
		langDir := filepath.Join(dir, "locales", lang)
		if err := os.MkdirAll(langDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(langDir, "common.json"), []byte(`{}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	target := filepath.Join(dir, "locales", "%slug%", "%original_file_name%")
	slugs, err := LanguageSlugsFromFilesystem(target)
	if err != nil {
		t.Fatal(err)
	}

	// Results are sorted alphabetically.
	want := []string{"de", "en", "fr"}
	if len(slugs) != len(want) {
		t.Fatalf("want %v, got %v", want, slugs)
	}
	for i, s := range slugs {
		if s != want[i] {
			t.Errorf("position %d: got %q, want %q", i, s, want[i])
		}
	}
}

func TestLanguageSlugsFromFilesystem_noSlugPlaceholder(t *testing.T) {
	_, err := LanguageSlugsFromFilesystem("locales/fixed/path.json")
	if err == nil {
		t.Error("expected error for target without slug placeholder")
	}
}

func TestLanguageSlugsFromFilesystem_noMatches(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "locales", "%slug%", "common.json")
	slugs, err := LanguageSlugsFromFilesystem(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(slugs) != 0 {
		t.Errorf("want empty, got %v", slugs)
	}
}

func TestLanguageSlugsFromFilesystem_deduplicates(t *testing.T) {
	// Multiple source files per language slug should appear only once.
	dir := t.TempDir()
	for _, lang := range []string{"en", "fr"} {
		langDir := filepath.Join(dir, "locales", lang)
		if err := os.MkdirAll(langDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"a.json", "b.json"} {
			if err := os.WriteFile(filepath.Join(langDir, name), []byte(`{}`), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	target := filepath.Join(dir, "locales", "%slug%", "%original_file_name%")
	slugs, err := LanguageSlugsFromFilesystem(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(slugs) != 2 {
		t.Errorf("want 2 unique slugs, got %v", slugs)
	}
}
