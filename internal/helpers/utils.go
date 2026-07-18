package helpers

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/sergey-pr/accentctl/internal/config"
)

// DocumentName strips directory and extension from a file path.
func DocumentName(filePath string) string {
	base := filepath.Base(filePath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// SourceLanguage returns the file's configured language, falling back to the
// slug extracted from the source path via the target template.
func SourceLanguage(file config.File, src string) string {
	if file.Language != "" {
		return file.Language
	}
	return LanguageFromPath(filepath.ToSlash(src), file.Target)
}

// ApplyTargetTemplate fills %slug%, %document_path% and %original_file_name% in
// the target pattern. The file name keeps the source's own extension, so non-JSON pulls land in the right file.
func ApplyTargetTemplate(target, language, src string) string {
	base := filepath.Base(src)
	r := strings.NewReplacer(
		"%slug%", language,
		"%document_path%", DocumentName(src),
		"%original_file_name%", base,
	)
	return r.Replace(target)
}

// LanguageFromPath extracts the language slug from a file path by matching it
// against the target template. Returns empty string if the slug cannot be determined.
func LanguageFromPath(filePath, target string) string {
	pattern := regexp.QuoteMeta(filepath.ToSlash(target))
	pattern = strings.ReplaceAll(pattern, regexp.QuoteMeta("%slug%"), "([^/]+)")
	pattern = strings.ReplaceAll(pattern, regexp.QuoteMeta("%document_path%"), "[^/]+")
	pattern = strings.ReplaceAll(pattern, regexp.QuoteMeta("%original_file_name%"), "[^/]+")

	re, err := regexp.Compile("^" + pattern + "$")
	if err != nil {
		return ""
	}

	normalised := filepath.ToSlash(filePath)
	matches := re.FindStringSubmatch(normalised)
	if len(matches) < 2 {
		return ""
	}
	return matches[1]
}

// LanguageSlugsFromFilesystem discovers language slugs by globbing the target
// template with %slug% replaced by * and extracting the slug from each match.
func LanguageSlugsFromFilesystem(target string) ([]string, error) {
	target = filepath.ToSlash(target)
	if !strings.Contains(target, "%slug%") {
		return nil, fmt.Errorf("target %q does not contain %%slug%%", target)
	}
	pattern := strings.ReplaceAll(target, "%slug%", "*")
	pattern = strings.ReplaceAll(pattern, "%document_path%", "*")
	pattern = strings.ReplaceAll(pattern, "%original_file_name%", "*")

	matches, err := doublestar.FilepathGlob(pattern)
	if err != nil {
		return nil, fmt.Errorf("could not glob %q: %w", pattern, err)
	}

	seen := map[string]bool{}
	var slugs []string
	for _, match := range matches {
		slug := LanguageFromPath(filepath.ToSlash(match), target)
		if slug != "" && !seen[slug] {
			seen[slug] = true
			slugs = append(slugs, slug)
		}
	}
	sort.Strings(slugs)
	return slugs, nil
}
