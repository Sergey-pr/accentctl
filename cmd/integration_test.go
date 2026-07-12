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

	"github.com/sergey-pr/accentctl/internal/api"
)

// setupProject chdirs into a fresh temp dir and writes an accent.json pointing
// at the fake server. Source files live in localization/en, targets follow
// localization/%slug%/%original_file_name%.
func setupProject(t *testing.T, apiURL string) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)

	cfg := fmt.Sprintf(`{
  "apiUrl": %q,
  "apiKey": %q,
  "files": [{
    "format": "json",
    "source": "localization/en/*.json",
    "target": "localization/%%slug%%/%%original_file_name%%"
  }]
}`, apiURL, fakeAPIKey)
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
