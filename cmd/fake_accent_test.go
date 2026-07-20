package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/sergey-pr/accentctl/internal/constants"
	"github.com/sergey-pr/accentctl/internal/helpers"
)

func init() {
	constants.RequestDelay = 0
}

const fakeAPIKey = "test-api-key"

// uploadCall records one POST to /sync or /add-translations.
type uploadCall struct {
	Endpoint string // "sync" | "add-translations"
	Document string
	Language string
	Mode     string // sync_type or merge_type
	KeyCount int    // number of leaf keys in the uploaded file
}

// fakeEntry is one translation plus Accent's `conflicted` flag: entries start
// conflicted, settle when reviewed, and merge_type=passive skips settled ones.
type fakeEntry struct {
	value      json.RawMessage
	conflicted bool
}

// fakeDoc is one Accent document: an ordered key set shared by all project
// languages, with per-language entries.
type fakeDoc struct {
	order []string                         // node keys in insertion order
	langs map[string]map[string]*fakeEntry // language -> node key -> entry
}

// fakeAccent is an in-memory fake of POST /sync, POST /add-translations and
// GET /export. As on the real server, the key set is shared by all project languages.
type fakeAccent struct {
	t          *testing.T
	mu         sync.Mutex
	languages  []string
	docs       map[string]*fakeDoc
	calls      []uploadCall
	srv        *httptest.Server
	failOn     map[string]bool  // endpoint -> respond 500 instead of serving
	onUploadFn func(uploadCall) // optional: runs after each recorded upload
}

func newFakeAccent(t *testing.T, languages ...string) *fakeAccent {
	f := &fakeAccent{
		t:         t,
		languages: languages,
		docs:      map[string]*fakeDoc{},
		failOn:    map[string]bool{},
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAccent) URL() string { return f.srv.URL }

// seed sets a document's state directly: seeded values count as reviewed, and
// languages left unseeded get an empty, still-conflicted placeholder.
func (f *fakeAccent) seed(document, language string, pairs [][2]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	doc := f.doc(document)
	for _, p := range pairs {
		key, value := p[0], p[1]
		if !f.inOrder(doc, key) {
			doc.order = append(doc.order, key)
			for _, lang := range f.languages {
				doc.langs[lang][key] = &fakeEntry{value: json.RawMessage(`""`), conflicted: true}
			}
		}
		doc.langs[language][key] = &fakeEntry{
			value:      json.RawMessage(fmt.Sprintf("%q", value)),
			conflicted: false,
		}
	}
}

// markReviewed models a reviewer correcting a string in Accent: it sets the
// value and clears the conflicted flag, which makes passive merges skip the key.
func (f *fakeAccent) markReviewed(document, language, key, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	doc, ok := f.docs[document]
	if !ok {
		f.t.Fatalf("markReviewed: unknown document %q", document)
	}
	entry, ok := doc.langs[language][key]
	if !ok {
		f.t.Fatalf("markReviewed: unknown key %q for language %q", key, language)
	}
	entry.value = json.RawMessage(fmt.Sprintf("%q", value))
	entry.conflicted = false
}

// get returns the decoded string value for a top-level key, or "" if absent.
func (f *fakeAccent) get(document, language, key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	doc, ok := f.docs[document]
	if !ok {
		return ""
	}
	entry, ok := doc.langs[language][key]
	if !ok {
		return ""
	}
	var s string
	_ = json.Unmarshal(entry.value, &s)
	return s
}

// keys returns the document's current key set in insertion order.
func (f *fakeAccent) keys(document string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	doc, ok := f.docs[document]
	if !ok {
		return nil
	}
	return append([]string{}, doc.order...)
}

// callsTo returns recorded uploads filtered by endpoint.
func (f *fakeAccent) callsTo(endpoint string) []uploadCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []uploadCall
	for _, c := range f.calls {
		if c.Endpoint == endpoint {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeAccent) doc(document string) *fakeDoc {
	doc, ok := f.docs[document]
	if !ok {
		doc = &fakeDoc{langs: map[string]map[string]*fakeEntry{}}
		for _, lang := range f.languages {
			doc.langs[lang] = map[string]*fakeEntry{}
		}
		f.docs[document] = doc
	}
	return doc
}

func (f *fakeAccent) inOrder(doc *fakeDoc, key string) bool {
	for _, k := range doc.order {
		if k == key {
			return true
		}
	}
	return false
}

// failEndpoint makes every later request to the given path respond 500, to
// simulate a fault partway through a command.
func (f *fakeAccent) failEndpoint(path string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failOn[path] = true
}

// onUpload registers a callback run after each recorded upload, for failing an
// endpoint only once a command has reached a particular phase.
func (f *fakeAccent) onUpload(fn func(uploadCall)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onUploadFn = fn
}

// recordCall appends the call and returns any registered upload callback, to be
// invoked by the caller once it has released the lock.
func (f *fakeAccent) recordCall(c uploadCall) func(uploadCall) {
	f.calls = append(f.calls, c)
	return f.onUploadFn
}

func (f *fakeAccent) handle(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+fakeAPIKey {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	f.mu.Lock()
	shouldFail := f.failOn[r.URL.Path]
	f.mu.Unlock()
	if shouldFail {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/sync":
		f.handleSync(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/add-translations":
		f.handleAddTranslations(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/export":
		f.handleExport(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeAccent) parseUpload(r *http.Request) (document, language string, nodes []helpers.NodeEntry, err error) {
	if err = r.ParseMultipartForm(32 << 20); err != nil {
		return "", "", nil, err
	}
	document = r.FormValue("document_path")
	language = r.FormValue("language")
	file, _, err := r.FormFile("file")
	if err != nil {
		return "", "", nil, err
	}
	defer func() {
		_ = file.Close()
	}()
	data, err := io.ReadAll(file)
	if err != nil {
		return "", "", nil, err
	}
	obj, err := helpers.ParseJSONObject(data)
	if err != nil {
		return "", "", nil, err
	}
	if obj != nil {
		nodes = helpers.CollectNodes(obj, nil)
	}
	return document, language, nodes, nil
}

func (f *fakeAccent) handleSync(w http.ResponseWriter, r *http.Request) {
	document, language, nodes, err := f.parseUpload(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	syncType := r.FormValue("sync_type")

	f.mu.Lock()
	doc := f.doc(document)
	fileSet := map[string]bool{}
	for _, n := range nodes {
		fileSet[helpers.NodeKey(n.Path)] = true
	}

	if syncType == "smart" {
		var kept []string
		for _, k := range doc.order {
			if fileSet[k] {
				kept = append(kept, k)
			} else {
				for _, lang := range f.languages {
					delete(doc.langs[lang], k)
				}
			}
		}
		doc.order = kept
	}

	// As on the real server, a synced key appears in every language holding
	// the source text, conflicted until a reviewer corrects it.
	for _, n := range nodes {
		k := helpers.NodeKey(n.Path)
		if f.inOrder(doc, k) {
			continue
		}
		doc.order = append(doc.order, k)
		for _, lang := range f.languages {
			doc.langs[lang][k] = &fakeEntry{value: n.Value, conflicted: true}
		}
	}

	call := uploadCall{
		Endpoint: "sync", Document: document, Language: language,
		Mode: syncType, KeyCount: len(nodes),
	}
	hook := f.recordCall(call)
	f.mu.Unlock()

	if hook != nil {
		hook(call)
	}
	w.WriteHeader(http.StatusOK)
}

func (f *fakeAccent) handleAddTranslations(w http.ResponseWriter, r *http.Request) {
	document, language, nodes, err := f.parseUpload(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	mergeType := r.FormValue("merge_type")

	f.mu.Lock()
	doc, ok := f.docs[document]
	if !ok {
		f.mu.Unlock()
		w.WriteHeader(http.StatusNotFound)
		return
	}
	values, ok := doc.langs[language]
	if !ok {
		f.mu.Unlock()
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// A merge never creates or removes keys, and passive skips reviewed entries.
	for _, n := range nodes {
		k := helpers.NodeKey(n.Path)
		entry, exists := values[k]
		if !exists {
			continue
		}
		if mergeType == "passive" && !entry.conflicted {
			continue
		}
		entry.value = n.Value
	}

	call := uploadCall{
		Endpoint: "add-translations", Document: document, Language: language,
		Mode: mergeType, KeyCount: len(nodes),
	}
	hook := f.recordCall(call)
	f.mu.Unlock()

	if hook != nil {
		hook(call)
	}
	w.WriteHeader(http.StatusOK)
}

func (f *fakeAccent) handleExport(w http.ResponseWriter, r *http.Request) {
	document := r.URL.Query().Get("document_path")
	language := r.URL.Query().Get("language")
	orderBy := r.URL.Query().Get("order_by")

	f.mu.Lock()
	defer f.mu.Unlock()

	doc, ok := f.docs[document]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	values, ok := doc.langs[language]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	order := append([]string{}, doc.order...)
	switch orderBy {
	case "key":
		sort.Strings(order)
	case "-key":
		sort.Sort(sort.Reverse(sort.StringSlice(order)))
	}

	var nodes []helpers.NodeEntry
	for _, k := range order {
		nodes = append(nodes, helpers.NodeEntry{
			Path:  strings.Split(k, "\x00"),
			Value: values[k].value,
		})
	}
	data, err := helpers.MarshalNodes(nodes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}
