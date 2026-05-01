package helpers

import (
	"encoding/json"
	"os"
	"testing"
)

func mustParse(t *testing.T, s string) *JSONObject {
	t.Helper()
	obj, err := ParseJSONObject([]byte(s))
	if err != nil {
		t.Fatalf("ParseJSONObject: %v", err)
	}
	if obj == nil {
		t.Fatal("ParseJSONObject returned nil for valid object")
	}
	return obj
}

func writeTempJSON(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp("", "tree-test-*.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	t.Cleanup(func() { _ = os.Remove(f.Name()) })
	return f.Name()
}

func jsonEqual(t *testing.T, got []byte, want string) {
	t.Helper()
	var a, b any
	if err := json.Unmarshal(got, &a); err != nil {
		t.Fatalf("got invalid JSON: %v", err)
	}
	if err := json.Unmarshal([]byte(want), &b); err != nil {
		t.Fatalf("want invalid JSON: %v", err)
	}
	ga, _ := json.Marshal(a)
	gb, _ := json.Marshal(b)
	if string(ga) != string(gb) {
		t.Errorf("JSON mismatch\ngot:  %s\nwant: %s", got, want)
	}
}

func TestNodeKey(t *testing.T) {
	tests := []struct {
		path []string
		want string
	}{
		{[]string{"a"}, "a"},
		{[]string{"a", "b"}, "a\x00b"},
		{[]string{"a", "b", "c"}, "a\x00b\x00c"},
		{[]string{}, ""},
	}
	for _, tt := range tests {
		if got := NodeKey(tt.path); got != tt.want {
			t.Errorf("NodeKey(%v) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestCollectNodes_flat(t *testing.T) {
	obj := mustParse(t, `{"a":"1","b":"2","c":"3"}`)
	nodes := CollectNodes(obj, nil)
	if len(nodes) != 3 {
		t.Fatalf("want 3 nodes, got %d", len(nodes))
	}
	paths := map[string]bool{}
	for _, n := range nodes {
		paths[NodeKey(n.Path)] = true
	}
	for _, key := range []string{"a", "b", "c"} {
		if !paths[key] {
			t.Errorf("missing key %q", key)
		}
	}
}

func TestCollectNodes_nested(t *testing.T) {
	obj := mustParse(t, `{"outer":{"inner":"val"}}`)
	nodes := CollectNodes(obj, nil)
	if len(nodes) != 1 {
		t.Fatalf("want 1 node, got %d", len(nodes))
	}
	if NodeKey(nodes[0].Path) != "outer\x00inner" {
		t.Errorf("unexpected path: %v", nodes[0].Path)
	}
}

func TestCollectNodes_mixedDepth(t *testing.T) {
	obj := mustParse(t, `{"a":"1","b":{"c":"2","d":"3"}}`)
	nodes := CollectNodes(obj, nil)
	if len(nodes) != 3 {
		t.Fatalf("want 3 nodes, got %d", len(nodes))
	}
}

func TestCollectNodes_empty(t *testing.T) {
	obj := mustParse(t, `{}`)
	nodes := CollectNodes(obj, nil)
	if len(nodes) != 0 {
		t.Errorf("want 0 nodes, got %d", len(nodes))
	}
}

func TestCollectNodes_preservesOrder(t *testing.T) {
	obj := mustParse(t, `{"z":"1","a":"2","m":"3"}`)
	nodes := CollectNodes(obj, nil)
	want := []string{"z", "a", "m"}
	for i, n := range nodes {
		if n.Path[0] != want[i] {
			t.Errorf("position %d: got %q, want %q", i, n.Path[0], want[i])
		}
	}
}

func TestMarshalNodes_parseMarshal(t *testing.T) {
	input := `{"a":"1","b":{"c":"2"}}`
	obj := mustParse(t, input)
	nodes := CollectNodes(obj, nil)
	got, err := MarshalNodes(nodes)
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, got, input)
}

func TestMarshalNodes_empty(t *testing.T) {
	got, err := MarshalNodes(nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "{}" {
		t.Errorf("want {}, got %s", got)
	}
}

func TestNewKeysChunksWithNodes_allNew(t *testing.T) {
	// No existing data all local keys are new.
	path := writeTempJSON(t, `{"a":"1","b":"2","c":"3"}`)
	paths, nodes, err := NewKeysChunksWithNodes(path, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 {
		t.Errorf("want 3 new nodes, got %d", len(nodes))
	}
	if len(paths) != 1 {
		t.Errorf("want 1 chunk, got %d", len(paths))
	}
	defer func() {
		_ = os.Remove(paths[0])
	}()

	data, _ := os.ReadFile(paths[0])
	jsonEqual(t, data, `{"a":"1","b":"2","c":"3"}`)
}

func TestNewKeysChunksWithNodes_noneNew(t *testing.T) {
	// Existing data matches local no new keys.
	content := `{"a":"1","b":"2"}`
	path := writeTempJSON(t, content)
	paths, nodes, err := NewKeysChunksWithNodes(path, []byte(content), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 0 || len(paths) != 0 {
		t.Errorf("want no chunks/nodes, got %d chunks, %d nodes", len(paths), len(nodes))
	}
}

func TestNewKeysChunksWithNodes_partialNew(t *testing.T) {
	// "a" already exists on server, "b" and "c" are new.
	existing := []byte(`{"a":"old"}`)
	path := writeTempJSON(t, `{"a":"1","b":"2","c":"3"}`)
	paths, nodes, err := NewKeysChunksWithNodes(path, existing, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, p := range paths {
			_ = os.Remove(p)
		}
	}()

	if len(nodes) != 2 {
		t.Errorf("want 2 new nodes, got %d", len(nodes))
	}
	// Chunk must contain existing key + new keys.
	data, _ := os.ReadFile(paths[0])
	jsonEqual(t, data, `{"a":"old","b":"2","c":"3"}`)
}

func TestNewKeysChunksWithNodes_chunking(t *testing.T) {
	// 4 new keys with chunkSize=2 -> 2 chunks, cumulative.
	path := writeTempJSON(t, `{"a":"1","b":"2","c":"3","d":"4"}`)
	paths, nodes, err := NewKeysChunksWithNodes(path, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, p := range paths {
			_ = os.Remove(p)
		}
	}()

	if len(nodes) != 4 {
		t.Errorf("want 4 new nodes, got %d", len(nodes))
	}
	if len(paths) != 2 {
		t.Errorf("want 2 chunks, got %d", len(paths))
	}

	// Chunk 1: first 2 new keys only (no existing).
	data0, _ := os.ReadFile(paths[0])
	jsonEqual(t, data0, `{"a":"1","b":"2"}`)

	// Chunk 2: cumulative all 4 keys.
	data1, _ := os.ReadFile(paths[1])
	jsonEqual(t, data1, `{"a":"1","b":"2","c":"3","d":"4"}`)
}

func TestNewKeysChunksWithNodes_chunkingWithExisting(t *testing.T) {
	// 2 existing + 4 new, chunkSize=2 -> 2 chunks, each cumulative with existing.
	existing := []byte(`{"x":"old","y":"old"}`)
	path := writeTempJSON(t, `{"x":"1","y":"2","a":"3","b":"4","c":"5","d":"6"}`)
	paths, nodes, err := NewKeysChunksWithNodes(path, existing, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, p := range paths {
			_ = os.Remove(p)
		}
	}()

	if len(nodes) != 4 {
		t.Errorf("want 4 new nodes, got %d", len(nodes))
	}
	if len(paths) != 2 {
		t.Fatalf("want 2 chunks, got %d", len(paths))
	}

	// Chunk 1: existing + first 2 new.
	data0, _ := os.ReadFile(paths[0])
	jsonEqual(t, data0, `{"x":"old","y":"old","a":"3","b":"4"}`)

	// Chunk 2: existing + all 4 new.
	data1, _ := os.ReadFile(paths[1])
	jsonEqual(t, data1, `{"x":"old","y":"old","a":"3","b":"4","c":"5","d":"6"}`)
}

func TestNewKeysChunksWithNodes_nestedKeys(t *testing.T) {
	// Nested JSON diff operates at node level.
	existing := []byte(`{"outer":{"a":"old"}}`)
	path := writeTempJSON(t, `{"outer":{"a":"1","b":"2"}}`)
	paths, nodes, err := NewKeysChunksWithNodes(path, existing, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, p := range paths {
			_ = os.Remove(p)
		}
	}()

	if len(nodes) != 1 {
		t.Errorf("want 1 new node (outer.b), got %d", len(nodes))
	}
	data, _ := os.ReadFile(paths[0])
	jsonEqual(t, data, `{"outer":{"a":"old","b":"2"}}`)
}

func TestNewKeysChunksWithNodes_forceMode(t *testing.T) {
	// nil existing -> force mode: all local keys treated as new.
	path := writeTempJSON(t, `{"a":"1","b":"2"}`)
	paths, nodes, err := NewKeysChunksWithNodes(path, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, p := range paths {
			_ = os.Remove(p)
		}
	}()

	if len(nodes) != 2 {
		t.Errorf("want 2 nodes in force mode, got %d", len(nodes))
	}
	data, _ := os.ReadFile(paths[0])
	jsonEqual(t, data, `{"a":"1","b":"2"}`)
}
