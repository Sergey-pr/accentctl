package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/sergey-pr/accentctl/internal/api"
)

// setupProject chdirs into a fresh temp dir and writes an accent.json pointing
// at the fake server. Source files live in localization/en, targets follow
// localization/%slug%/%original_file_name%.
func setupProject(t *testing.T, apiURL string) {
	t.Helper()
	setupProjectWithFormat(t, apiURL, "json")
}

func setupProjectWithFormat(t *testing.T, apiURL, format string) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)

	cfg := fmt.Sprintf(`{
  "apiUrl": %q,
  "apiKey": %q,
  "files": [{
    "format": %q,
    "source": "localization/en/*.json",
    "target": "localization/%%slug%%/%%original_file_name%%"
  }]
}`, apiURL, fakeAPIKey, format)
	if err := os.WriteFile("accent.json", []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeLocalFile(t *testing.T, slug, doc, content string) {
	t.Helper()
	path := filepath.Join("localization", slug, doc+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readLocalFile(t *testing.T, slug, doc string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("localization", slug, doc+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func resetFlags(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		syncOrderBy = "key"
		syncForce = false
		syncYes = false
		syncTranslationsOnly = false
		pullOrderBy = "key"
	})
}

// manyPairs returns n key/value pairs k000..k<n-1> with the given value prefix.
func manyPairs(n int, valuePrefix string) [][2]string {
	pairs := make([][2]string, n)
	for i := range pairs {
		pairs[i] = [2]string{fmt.Sprintf("k%03d", i), fmt.Sprintf("%s%03d", valuePrefix, i)}
	}
	return pairs
}

func pairsJSON(t *testing.T, pairs [][2]string) string {
	t.Helper()
	m := map[string]string{}
	keys := make([]string, 0, len(pairs))
	for _, p := range pairs {
		m[p[0]] = p[1]
		keys = append(keys, p[0])
	}
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		vb, _ := json.Marshal(m[k])
		b.Write(kb)
		b.WriteByte(':')
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.String()
}

func keyCounts(calls []uploadCall) []int {
	var out []int
	for _, c := range calls {
		out = append(out, c.KeyCount)
	}
	return out
}

// The JSON-only commands must reject a non-JSON format before touching the
// network, rather than failing later inside a JSON parser.
func TestNonJSONFormatFailsFastOnJSONOnlyCommands(t *testing.T) {
	commands := map[string]func(*cobra.Command, []string) error{
		"sync":    runSync,
		"cleanup": runCleanup,
		"status":  runStatus,
	}
	for name, run := range commands {
		t.Run(name, func(t *testing.T) {
			resetFlags(t)
			fake := newFakeAccent(t, "en", "fr")
			setupProjectWithFormat(t, fake.URL(), "yaml")
			writeLocalFile(t, "en", "app", `{"a":"A"}`)
			writeLocalFile(t, "fr", "app", `{"a":"A-fr"}`)

			err := run(nil, nil)
			if err == nil {
				t.Fatalf("%s with format yaml succeeded, want a clear error", name)
			}
			for _, want := range []string{name, `"json"`, `"yaml"`, "pull"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to mention %s", err, want)
				}
			}
			if calls := len(fake.callsTo("sync")) + len(fake.callsTo("add-translations")); calls != 0 {
				t.Errorf("%s made %d upload(s) before failing, want 0", name, calls)
			}
		})
	}
}

func TestJSONFormatAndUnsetFormatAreAccepted(t *testing.T) {
	for _, format := range []string{"json", ""} {
		t.Run("format="+format, func(t *testing.T) {
			resetFlags(t)
			fake := newFakeAccent(t, "en", "fr")
			fake.seed("app", "en", [][2]string{{"a", "A"}})
			fake.seed("app", "fr", [][2]string{{"a", "A-fr"}})
			setupProjectWithFormat(t, fake.URL(), format)
			writeLocalFile(t, "en", "app", `{"a":"A"}`)
			writeLocalFile(t, "fr", "app", `{"a":"A-fr"}`)

			if err := runStatus(nil, nil); err != nil {
				t.Errorf("status with format %q failed: %v", format, err)
			}
		})
	}
}

// A local file that is valid JSON but not an object used to be handled three
// different ways; every command must now name the file and stop.
func TestNonObjectLocalFileIsRejectedConsistently(t *testing.T) {
	commands := map[string]func(*cobra.Command, []string) error{
		"sync":    runSync,
		"cleanup": runCleanup,
		"status":  runStatus,
	}
	for name, run := range commands {
		t.Run(name, func(t *testing.T) {
			resetFlags(t)
			fake := newFakeAccent(t, "en", "fr")
			fake.seed("app", "en", [][2]string{{"a", "A"}})
			setupProject(t, fake.URL())
			writeLocalFile(t, "en", "app", `["a","b"]`)

			err := run(nil, nil)
			if err == nil {
				t.Fatalf("%s on a top-level JSON array succeeded, want an error", name)
			}
			if !strings.Contains(err.Error(), "not a JSON object") {
				t.Errorf("error = %q, want it to say the file is not a JSON object", err)
			}
			if !strings.Contains(err.Error(), filepath.Join("localization", "en", "app.json")) {
				t.Errorf("error = %q, want it to name the offending file", err)
			}
		})
	}
}

func TestMalformedLocalFileIsRejected(t *testing.T) {
	resetFlags(t)
	fake := newFakeAccent(t, "en", "fr")
	fake.seed("app", "en", [][2]string{{"a", "A"}})
	setupProject(t, fake.URL())
	writeLocalFile(t, "en", "app", `{"a": `)

	err := runSync(nil, nil)
	if err == nil {
		t.Fatal("sync on malformed JSON succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("error = %q, want it to say the JSON is invalid", err)
	}
}

func TestSyncPushesNewKeysAndTheirTranslations(t *testing.T) {
	resetFlags(t)
	fake := newFakeAccent(t, "en", "fr")
	fake.seed("app", "en", [][2]string{{"a", "A"}})
	fake.seed("app", "fr", [][2]string{{"a", "A-fr"}})

	setupProject(t, fake.URL())
	writeLocalFile(t, "en", "app", `{"a":"A","b":"B"}`)
	writeLocalFile(t, "fr", "app", `{"a":"A-fr","b":"B-fr"}`)

	if err := runSync(nil, nil); err != nil {
		t.Fatal(err)
	}

	if got := fake.get("app", "en", "b"); got != "B" {
		t.Errorf("server en.b = %q, want %q", got, "B")
	}
	if got := fake.get("app", "fr", "b"); got != "B-fr" {
		t.Errorf("server fr.b = %q, want %q", got, "B-fr")
	}
	if got := fake.get("app", "fr", "a"); got != "A-fr" {
		t.Errorf("server fr.a = %q, want %q (existing translation must not change)", got, "A-fr")
	}

	syncCalls := fake.callsTo("sync")
	if len(syncCalls) != 1 || syncCalls[0].Mode != "passive" {
		t.Errorf("sync calls = %+v, want one passive call", syncCalls)
	}
	addCalls := fake.callsTo("add-translations")
	if len(addCalls) != 1 || addCalls[0].Mode != "force" || addCalls[0].Language != "fr" || addCalls[0].KeyCount != 1 {
		t.Errorf("add-translations calls = %+v, want one force call for fr with 1 key", addCalls)
	}

	if got := readLocalFile(t, "fr", "app"); !strings.Contains(got, `"b": "B-fr"`) {
		t.Errorf("pulled fr file = %q, want it to contain b", got)
	}
}

func TestSyncPushesNewKeyTranslationsInDisjointChunks(t *testing.T) {
	resetFlags(t)
	const n = 501 // 2 full chunks of 250 + 1

	fake := newFakeAccent(t, "en", "fr")
	setupProject(t, fake.URL())
	writeLocalFile(t, "en", "app", pairsJSON(t, manyPairs(n, "v")))
	writeLocalFile(t, "fr", "app", pairsJSON(t, manyPairs(n, "f")))

	if err := runSync(nil, nil); err != nil {
		t.Fatal(err)
	}

	// A merge never removes keys absent from the uploaded file, so each key is
	// sent exactly once rather than in growing prefixes.
	addCalls := fake.callsTo("add-translations")
	if got, want := keyCounts(addCalls), []int{250, 250, 1}; !reflect.DeepEqual(got, want) {
		t.Errorf("add-translations sizes = %v, want %v", got, want)
	}
	for _, c := range addCalls {
		if c.Language != "fr" || c.Mode != "force" {
			t.Errorf("add-translations call = %+v, want language fr with force merge", c)
		}
	}

	for _, k := range []string{"k000", "k250", "k500"} {
		if got, want := fake.get("app", "fr", k), strings.Replace(k, "k", "f", 1); got != want {
			t.Errorf("server fr.%s = %q, want %q", k, got, want)
		}
	}
}

func TestSyncNoNewKeysDoesNotUpload(t *testing.T) {
	resetFlags(t)
	fake := newFakeAccent(t, "en", "fr")
	fake.seed("app", "en", [][2]string{{"a", "A"}})
	fake.seed("app", "fr", [][2]string{{"a", "A-fr"}})

	setupProject(t, fake.URL())
	writeLocalFile(t, "en", "app", `{"a":"A"}`)
	writeLocalFile(t, "fr", "app", `{"a":"A-fr"}`)

	if err := runSync(nil, nil); err != nil {
		t.Fatal(err)
	}

	if calls := fake.callsTo("sync"); len(calls) != 0 {
		t.Errorf("sync calls = %+v, want none", calls)
	}
	if calls := fake.callsTo("add-translations"); len(calls) != 0 {
		t.Errorf("add-translations calls = %+v, want none", calls)
	}
}

func TestSyncForceDeletesThenReuploadsInChunks(t *testing.T) {
	resetFlags(t)
	const n = 501 // 2 full chunks of 250 + 1

	fake := newFakeAccent(t, "en", "fr")
	fake.seed("app", "en", manyPairs(n, "old"))

	setupProject(t, fake.URL())
	writeLocalFile(t, "en", "app", pairsJSON(t, manyPairs(n, "v")))
	writeLocalFile(t, "fr", "app", pairsJSON(t, manyPairs(n, "f")))

	syncForce = true
	syncYes = true
	if err := runSync(nil, nil); err != nil {
		t.Fatal(err)
	}

	syncCalls := fake.callsTo("sync")
	var smart, passive []uploadCall
	for _, c := range syncCalls {
		switch c.Mode {
		case "smart":
			smart = append(smart, c)
		case "passive":
			passive = append(passive, c)
		}
	}
	// Delete phase: each smart upload holds the keys NOT yet deleted, ending
	// with an empty file that clears the rest.
	if got, want := keyCounts(smart), []int{n - 250, n - 500, 0}; !reflect.DeepEqual(got, want) {
		t.Errorf("smart (delete) upload sizes = %v, want %v", got, want)
	}
	// Re-upload phase: cumulative chunks of the full local file.
	if got, want := keyCounts(passive), []int{250, 500, n}; !reflect.DeepEqual(got, want) {
		t.Errorf("passive (re-upload) sizes = %v, want %v", got, want)
	}

	addCalls := fake.callsTo("add-translations")
	if got, want := keyCounts(addCalls), []int{250, 500, n}; !reflect.DeepEqual(got, want) {
		t.Errorf("add-translations sizes = %v, want %v", got, want)
	}
	for _, c := range addCalls {
		if c.Language != "fr" || c.Mode != "force" {
			t.Errorf("add-translations call = %+v, want language fr with force merge", c)
		}
	}

	if got := len(fake.keys("app")); got != n {
		t.Errorf("server key count = %d, want %d", got, n)
	}
	if got := fake.get("app", "en", "k000"); got != "v000" {
		t.Errorf("server en.k000 = %q, want %q", got, "v000")
	}
	if got := fake.get("app", "fr", "k500"); got != "f500" {
		t.Errorf("server fr.k500 = %q, want %q", got, "f500")
	}
}

func TestSyncForceWithoutYesAbortsOnNonTTY(t *testing.T) {
	resetFlags(t)
	fake := newFakeAccent(t, "en", "fr")
	fake.seed("app", "en", [][2]string{{"a", "A"}})
	fake.seed("app", "fr", [][2]string{{"a", "A-fr"}})

	setupProject(t, fake.URL())
	writeLocalFile(t, "en", "app", `{"a":"A"}`)
	writeLocalFile(t, "fr", "app", `{"a":"A-fr"}`)

	syncForce = true
	err := runSync(nil, nil)
	if err == nil {
		t.Fatal("expected --force without --yes on a non-TTY to abort")
	}

	if calls := fake.callsTo("sync"); len(calls) != 0 {
		t.Errorf("aborted --force made %d sync upload(s), want 0", len(calls))
	}
	if got := fake.get("app", "en", "a"); got != "A" {
		t.Errorf("server en.a = %q, want %q (must be untouched)", got, "A")
	}
}

// interruptSyncAfterKeyUpload reproduces the state left by a sync that died
// between its "Syncing files" and "Adding translations" phases: the new source
// keys are on the server (and so exist in every language) but no translation
// was ever pushed for them.
func interruptSyncAfterKeyUpload(t *testing.T, fake *fakeAccent) {
	t.Helper()
	client := api.New(fake.URL(), fakeAPIKey, false)
	src := filepath.Join("localization", "en", "app.json")
	if _, _, err := syncFileChunked(client, src, "app", "json", "en", "key", false); err != nil {
		t.Fatal(err)
	}
}

func TestSyncTranslationsOnlyRecoversInterruptedSync(t *testing.T) {
	resetFlags(t)
	fake := newFakeAccent(t, "en", "fr")
	fake.seed("app", "en", [][2]string{{"a", "A"}})
	fake.seed("app", "fr", [][2]string{{"a", "A-fr"}})

	setupProject(t, fake.URL())
	writeLocalFile(t, "en", "app", `{"a":"A","b":"B"}`)
	writeLocalFile(t, "fr", "app", `{"a":"A-fr","b":"B-fr"}`)

	interruptSyncAfterKeyUpload(t, fake)

	// The interrupted sync left fr.b holding the English source text, which is
	// why a plain re-run cannot detect it: the key is present, just untranslated.
	if got := fake.get("app", "fr", "b"); got != "B" {
		t.Fatalf("after interrupted sync, server fr.b = %q, want the source text %q", got, "B")
	}

	syncCallsBefore := len(fake.callsTo("sync"))

	syncTranslationsOnly = true
	if err := runSync(nil, nil); err != nil {
		t.Fatal(err)
	}

	if got := fake.get("app", "fr", "b"); got != "B-fr" {
		t.Errorf("server fr.b = %q, want %q (recovery must fill the untranslated key)", got, "B-fr")
	}

	// Recovery must not touch keys, in either direction.
	got := fake.keys("app")
	sort.Strings(got)
	if want := []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("server keys after recovery = %v, want %v", got, want)
	}
	if n := len(fake.callsTo("sync")) - syncCallsBefore; n != 0 {
		t.Errorf("--translations-only made %d sync upload(s), want 0", n)
	}
	for _, c := range fake.callsTo("add-translations") {
		if c.Mode != "passive" {
			t.Errorf("add-translations call = %+v, want a passive merge", c)
		}
	}

	if got := readLocalFile(t, "fr", "app"); !strings.Contains(got, `"b": "B-fr"`) {
		t.Errorf("pulled fr file = %q, want it to contain b", got)
	}
}

// TestInterruptedSyncIsNotFixedByPlainRerun pins down the gap that
// --translations-only exists to close. A plain re-run does not recover an
// interrupted sync, and its pull phase overwrites the local translations that
// recovery needs -- so recovery has to run before any pull.
func TestInterruptedSyncIsNotFixedByPlainRerun(t *testing.T) {
	resetFlags(t)
	fake := newFakeAccent(t, "en", "fr")
	fake.seed("app", "en", [][2]string{{"a", "A"}})
	fake.seed("app", "fr", [][2]string{{"a", "A-fr"}})

	setupProject(t, fake.URL())
	writeLocalFile(t, "en", "app", `{"a":"A","b":"B"}`)
	writeLocalFile(t, "fr", "app", `{"a":"A-fr","b":"B-fr"}`)

	interruptSyncAfterKeyUpload(t, fake)

	if err := runSync(nil, nil); err != nil {
		t.Fatal(err)
	}

	if got := fake.get("app", "fr", "b"); got != "B" {
		t.Errorf("server fr.b = %q, want the still-unrecovered source text %q", got, "B")
	}
	if calls := fake.callsTo("add-translations"); len(calls) != 0 {
		t.Errorf("plain re-run made %d add-translations call(s), want 0: the new-key diff finds nothing", len(calls))
	}
	if got := readLocalFile(t, "fr", "app"); strings.Contains(got, "B-fr") {
		t.Errorf("local fr file = %q; expected the pull phase to have overwritten B-fr with the source text", got)
	}
}

func TestSyncFailureAfterKeyUploadSuggestsRecovery(t *testing.T) {
	resetFlags(t)
	fake := newFakeAccent(t, "en", "fr")
	fake.seed("app", "en", [][2]string{{"a", "A"}})
	fake.seed("app", "fr", [][2]string{{"a", "A-fr"}})

	setupProject(t, fake.URL())
	writeLocalFile(t, "en", "app", `{"a":"A","b":"B"}`)
	writeLocalFile(t, "fr", "app", `{"a":"A-fr","b":"B-fr"}`)

	// Keys upload fine; pushing their translations is what dies.
	fake.failEndpoint("/add-translations")

	err := runSync(nil, nil)
	if err == nil {
		t.Fatal("expected sync to fail when /add-translations errors")
	}
	if !strings.Contains(err.Error(), "--translations-only") {
		t.Errorf("error = %q, want it to name the recovery command", err)
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("error = %q, want it to preserve the underlying failure", err)
	}

	// The failure must stop short of the pull phase, or it destroys the local
	// translations the suggested recovery depends on.
	if got := readLocalFile(t, "fr", "app"); !strings.Contains(got, "B-fr") {
		t.Errorf("local fr file = %q, want B-fr intact: a failed sync must not reach the pull phase", got)
	}
}

func TestSyncFailureBeforeAnyKeyUploadDoesNotSuggestRecovery(t *testing.T) {
	resetFlags(t)
	fake := newFakeAccent(t, "en", "fr")
	fake.seed("app", "en", [][2]string{{"a", "A"}})
	fake.seed("app", "fr", [][2]string{{"a", "A-fr"}})

	setupProject(t, fake.URL())
	writeLocalFile(t, "en", "app", `{"a":"A","b":"B"}`)
	writeLocalFile(t, "fr", "app", `{"a":"A-fr","b":"B-fr"}`)

	// Nothing reaches the server, so no recovery is owed.
	fake.failEndpoint("/export")

	err := runSync(nil, nil)
	if err == nil {
		t.Fatal("expected sync to fail when /export errors")
	}
	if strings.Contains(err.Error(), "--translations-only") {
		t.Errorf("error = %q, must not suggest recovery when no keys were uploaded", err)
	}
}

func TestSyncFailureAfterTranslationsPushedDoesNotSuggestRecovery(t *testing.T) {
	resetFlags(t)
	fake := newFakeAccent(t, "en", "fr")
	fake.seed("app", "en", [][2]string{{"a", "A"}})
	fake.seed("app", "fr", [][2]string{{"a", "A-fr"}})

	setupProject(t, fake.URL())
	writeLocalFile(t, "en", "app", `{"a":"A","b":"B"}`)
	writeLocalFile(t, "fr", "app", `{"a":"A-fr","b":"B-fr"}`)

	// Let the keys and translations land, then break the pull phase. The
	// translations are safely on the server, so the window is already closed.
	fake.onUpload(func(c uploadCall) {
		if c.Endpoint == "add-translations" {
			fake.failEndpoint("/export")
		}
	})

	err := runSync(nil, nil)
	if err == nil {
		t.Fatal("expected sync to fail when the pull phase errors")
	}
	if strings.Contains(err.Error(), "--translations-only") {
		t.Errorf("error = %q, must not suggest recovery once translations are pushed", err)
	}
	if got := fake.get("app", "fr", "b"); got != "B-fr" {
		t.Errorf("server fr.b = %q, want %q", got, "B-fr")
	}
}

func TestSyncTranslationsOnlyPreservesReviewedTranslations(t *testing.T) {
	resetFlags(t)
	fake := newFakeAccent(t, "en", "fr")
	fake.seed("app", "en", [][2]string{{"a", "A"}})
	fake.seed("app", "fr", [][2]string{{"a", "A-fr"}})

	setupProject(t, fake.URL())
	writeLocalFile(t, "en", "app", `{"a":"A","b":"B"}`)
	writeLocalFile(t, "fr", "app", `{"a":"A-fr","b":"B-fr"}`)

	interruptSyncAfterKeyUpload(t, fake)

	// A reviewer corrects the new key in Accent before recovery runs, and edits
	// a pre-existing one. Neither may be clobbered by the stale local values.
	fake.markReviewed("app", "fr", "b", "B-reviewed")
	fake.markReviewed("app", "fr", "a", "A-reviewed")

	syncTranslationsOnly = true
	if err := runSync(nil, nil); err != nil {
		t.Fatal(err)
	}

	if got := fake.get("app", "fr", "b"); got != "B-reviewed" {
		t.Errorf("server fr.b = %q, want %q (a reviewed string must survive recovery)", got, "B-reviewed")
	}
	if got := fake.get("app", "fr", "a"); got != "A-reviewed" {
		t.Errorf("server fr.a = %q, want %q (a reviewed string must survive recovery)", got, "A-reviewed")
	}
}

func TestSyncTranslationsOnlyRejectsForce(t *testing.T) {
	resetFlags(t)
	fake := newFakeAccent(t, "en", "fr")
	fake.seed("app", "en", [][2]string{{"a", "A"}})

	setupProject(t, fake.URL())
	writeLocalFile(t, "en", "app", `{"a":"A"}`)
	writeLocalFile(t, "fr", "app", `{"a":"A-fr"}`)

	syncForce = true
	syncYes = true
	syncTranslationsOnly = true
	if err := runSync(nil, nil); err == nil {
		t.Fatal("expected --force with --translations-only to be rejected")
	}
	if calls := fake.callsTo("sync"); len(calls) != 0 {
		t.Errorf("rejected run made %d sync upload(s), want 0", len(calls))
	}
	if calls := fake.callsTo("add-translations"); len(calls) != 0 {
		t.Errorf("rejected run made %d add-translations call(s), want 0", len(calls))
	}
}

func TestSyncTranslationsOnlyChunksLargeFiles(t *testing.T) {
	resetFlags(t)
	const n = 501 // 2 full chunks of 250 + 1

	fake := newFakeAccent(t, "en", "fr")
	setupProject(t, fake.URL())
	writeLocalFile(t, "en", "app", pairsJSON(t, manyPairs(n, "v")))
	writeLocalFile(t, "fr", "app", pairsJSON(t, manyPairs(n, "f")))

	interruptSyncAfterKeyUpload(t, fake)

	syncTranslationsOnly = true
	if err := runSync(nil, nil); err != nil {
		t.Fatal(err)
	}

	// Disjoint chunks: a merge only touches keys present in the uploaded file,
	// so unlike the sync path these do not need to accumulate.
	addCalls := fake.callsTo("add-translations")
	if got, want := keyCounts(addCalls), []int{250, 250, 1}; !reflect.DeepEqual(got, want) {
		t.Errorf("add-translations sizes = %v, want %v", got, want)
	}
	if got := fake.get("app", "fr", "k500"); got != "f500" {
		t.Errorf("server fr.k500 = %q, want %q", got, "f500")
	}
	if got := fake.get("app", "fr", "k000"); got != "f000" {
		t.Errorf("server fr.k000 = %q, want %q", got, "f000")
	}
}

func TestCleanupRemovesOrphanedKeysInChunks(t *testing.T) {
	resetFlags(t)
	const orphans = 501

	fake := newFakeAccent(t, "en", "fr")
	fake.seed("app", "en", [][2]string{{"a", "A"}, {"b", "B"}})
	fake.seed("app", "fr", [][2]string{{"a", "A-fr"}, {"b", "B-fr"}})
	fake.seed("app", "en", manyPairs(orphans, "orphan"))

	setupProject(t, fake.URL())
	writeLocalFile(t, "en", "app", `{"a":"A","b":"B"}`)
	writeLocalFile(t, "fr", "app", `{"a":"A-fr","b":"B-fr"}`)

	if err := runCleanup(nil, nil); err != nil {
		t.Fatal(err)
	}

	// Each upload = 2 local keys + orphans not yet removed.
	syncCalls := fake.callsTo("sync")
	if got, want := keyCounts(syncCalls), []int{2 + orphans - 250, 2 + orphans - 500, 2}; !reflect.DeepEqual(got, want) {
		t.Errorf("cleanup upload sizes = %v, want %v", got, want)
	}
	for _, c := range syncCalls {
		if c.Mode != "smart" {
			t.Errorf("cleanup call = %+v, want smart sync", c)
		}
	}

	got := fake.keys("app")
	sort.Strings(got)
	if want := []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("server keys after cleanup = %v, want %v", got, want)
	}
	if got := fake.get("app", "fr", "a"); got != "A-fr" {
		t.Errorf("server fr.a = %q, want %q (cleanup must not touch surviving translations)", got, "A-fr")
	}
}

func TestCleanupNoOrphansDoesNotUpload(t *testing.T) {
	resetFlags(t)
	fake := newFakeAccent(t, "en", "fr")
	fake.seed("app", "en", [][2]string{{"a", "A"}})
	fake.seed("app", "fr", [][2]string{{"a", "A-fr"}})

	setupProject(t, fake.URL())
	writeLocalFile(t, "en", "app", `{"a":"A"}`)
	writeLocalFile(t, "fr", "app", `{"a":"A-fr"}`)

	if err := runCleanup(nil, nil); err != nil {
		t.Fatal(err)
	}

	if calls := fake.callsTo("sync"); len(calls) != 0 {
		t.Errorf("sync calls = %+v, want none", calls)
	}
}

func TestPullSkipsLanguagesMissingOnServer(t *testing.T) {
	resetFlags(t)
	fake := newFakeAccent(t, "en") // project has no fr
	fake.seed("app", "en", [][2]string{{"a", "A"}})

	setupProject(t, fake.URL())
	writeLocalFile(t, "en", "app", `{"a":"A"}`)
	const frBefore = `{"a":"A-fr-local"}`
	writeLocalFile(t, "fr", "app", frBefore)

	if err := runPull(nil, nil); err != nil {
		t.Fatal(err)
	}

	if got := readLocalFile(t, "fr", "app"); got != frBefore {
		t.Errorf("fr file was modified on 404: %q, want untouched %q", got, frBefore)
	}
}

func TestPullWritesSortedFiles(t *testing.T) {
	resetFlags(t)
	fake := newFakeAccent(t, "en")
	fake.seed("app", "en", [][2]string{{"b", "B"}, {"a", "A"}})

	setupProject(t, fake.URL())
	writeLocalFile(t, "en", "app", `{}`)

	if err := runPull(nil, nil); err != nil {
		t.Fatal(err)
	}

	want := "{\n  \"a\": \"A\",\n  \"b\": \"B\"\n}\n"
	if got := readLocalFile(t, "en", "app"); got != want {
		t.Errorf("pulled file = %q, want %q", got, want)
	}
}

func TestStatusCountsPushAndDelete(t *testing.T) {
	resetFlags(t)
	fake := newFakeAccent(t, "en", "fr")
	fake.seed("app", "en", [][2]string{{"a", "A"}, {"c", "C"}})
	fake.seed("app", "fr", [][2]string{{"a", "A-fr"}, {"c", "C-fr"}})

	setupProject(t, fake.URL())
	writeLocalFile(t, "en", "app", `{"a":"A","b":"B"}`)
	writeLocalFile(t, "fr", "app", `{"a":"A-fr","b":"B-fr"}`)

	client := api.New(fake.URL(), fakeAPIKey, false)
	toPush, toDelete, err := diffWithAccent(client, filepath.Join("localization", "en", "app.json"), "app", "json", "en")
	if err != nil {
		t.Fatal(err)
	}
	if toPush != 1 || toDelete != 1 {
		t.Errorf("diffWithAccent = (push %d, delete %d), want (1, 1)", toPush, toDelete)
	}

	if err := runStatus(nil, nil); err != nil {
		t.Fatal(err)
	}
}
