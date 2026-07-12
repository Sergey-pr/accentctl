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

// fakeDoc is one Accent document: an ordered key set shared by all project
// languages, with per-language values.
type fakeDoc struct {
	order []string                              // node keys in insertion order
	langs map[string]map[string]json.RawMessage // language -> node key -> value
}

// fakeAccent is an in-memory fake of the three Accent endpoints used by the
// CLI: POST /sync, POST /add-translations, GET /export. Like the real server,
// the key set is project-wide: adding a key via one language creates an empty
// entry in every other project language, and removing a key removes it
// everywhere.
type fakeAccent struct {
	t         *testing.T
	mu        sync.Mutex
	languages []string
	docs      map[string]*fakeDoc
	calls     []uploadCall
	srv       *httptest.Server
}

func newFakeAccent(t *testing.T, languages ...string) *fakeAccent {
	f := &fakeAccent{
		t:         t,
		languages: languages,
		docs:      map[string]*fakeDoc{},
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAccent) URL() string { return f.srv.URL }

// seed sets a document's state directly, bypassing the endpoints. Keys are
// added to the shared order on first sight; other languages get "" values.
func (f *fakeAccent) seed(document, language string, pairs [][2]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	doc := f.doc(document)
	for _, p := range pairs {
		key, value := p[0], p[1]
		if _, exists := doc.langs[language][key]; !exists && !f.inOrder(doc, key) {
			doc.order = append(doc.order, key)
			for _, lang := range f.languages {
				doc.langs[lang][key] = json.RawMessage(`""`)
			}
		}
		doc.langs[language][key] = json.RawMessage(fmt.Sprintf("%q", value))
	}
}

// get returns the decoded string value for a top-level key, or "" if absent.
func (f *fakeAccent) get(document, language, key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	doc, ok := f.docs[document]
	if !ok {
		return ""
	}
	raw, ok := doc.langs[language][key]
	if !ok {
		return ""
	}
	var s string
	_ = json.Unmarshal(raw, &s)
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
		doc = &fakeDoc{langs: map[string]map[string]json.RawMessage{}}
		for _, lang := range f.languages {
			doc.langs[lang] = map[string]json.RawMessage{}
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

func (f *fakeAccent) handle(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+fakeAPIKey {
		w.WriteHeader(http.StatusUnauthorized)
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

	for _, n := range nodes {
		k := helpers.NodeKey(n.Path)
		if f.inOrder(doc, k) {
			continue
		}
		doc.order = append(doc.order, k)
		for _, lang := range f.languages {
			if lang == language {
				doc.langs[lang][k] = n.Value
			} else {
				doc.langs[lang][k] = json.RawMessage(`""`)
			}
		}
	}

	f.calls = append(f.calls, uploadCall{
		Endpoint: "sync", Document: document, Language: language,
		Mode: syncType, KeyCount: len(nodes),
	})
	f.mu.Unlock()

	writePeekResult(w)
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

	for _, n := range nodes {
		k := helpers.NodeKey(n.Path)
		current, exists := values[k]
		if !exists {
			continue
		}
		if mergeType == "passive" && string(current) != `""` {
			continue
		}
		values[k] = n.Value
	}

	f.calls = append(f.calls, uploadCall{
		Endpoint: "add-translations", Document: document, Language: language,
		Mode: mergeType, KeyCount: len(nodes),
	})
	f.mu.Unlock()

	writePeekResult(w)
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
			Value: values[k],
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

func writePeekResult(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"data":{"new_count":0,"updated_count":0,"removed_count":0,"conflicts_count":0}}`))
}
