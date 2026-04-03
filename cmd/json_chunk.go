package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
)

// jsonObject holds top-level keys in insertion order plus their raw values.
type jsonObject struct {
	keys   []string
	values map[string]json.RawMessage
}

// parseJSONObject parses a JSON object preserving insertion order.
// Returns nil (no error) if the input is not a JSON object.
func parseJSONObject(data []byte) (*jsonObject, error) {
	dec := json.NewDecoder(bytes.NewReader(data))

	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, nil // not an object
	}

	obj := &jsonObject{values: map[string]json.RawMessage{}}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key := keyTok.(string)

		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		obj.keys = append(obj.keys, key)
		obj.values[key] = raw
	}
	return obj, nil
}

// marshalObject serialises an object keeping keys in the supplied order.
func marshalObject(keys []string, values map[string]json.RawMessage) []byte {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(values[k])
	}
	buf.WriteByte('}')
	return buf.Bytes()
}

// --- leaf-level diff --------------------------------------------------------

// leafEntry is a single leaf key path and its raw JSON value.
type leafEntry struct {
	path  []string
	value json.RawMessage
}

// leafKey returns a map-safe string for a path.
// Uses \x00 as separator — invalid in JSON string content.
func leafKey(path []string) string { return strings.Join(path, "\x00") }

// collectLeaves recursively gathers all leaf (non-object) entries from obj.
func collectLeaves(obj *jsonObject, prefix []string) []leafEntry {
	var out []leafEntry
	for _, k := range obj.keys {
		path := append(append([]string{}, prefix...), k)
		child, _ := parseJSONObject(obj.values[k])
		if child != nil {
			out = append(out, collectLeaves(child, path)...)
		} else {
			out = append(out, leafEntry{path: path, value: obj.values[k]})
		}
	}
	return out
}

// --- tree builder for chunk reconstruction ----------------------------------

type treeNode struct {
	keys     []string
	children map[string]*treeNode // nil for leaves
	value    json.RawMessage      // set for leaves
}

func buildTree(leaves []leafEntry) *treeNode {
	root := &treeNode{children: map[string]*treeNode{}}
	for _, leaf := range leaves {
		node := root
		for i, k := range leaf.path {
			if i == len(leaf.path)-1 {
				node.keys = append(node.keys, k)
				node.children[k] = &treeNode{value: leaf.value}
			} else {
				if _, exists := node.children[k]; !exists {
					node.keys = append(node.keys, k)
					node.children[k] = &treeNode{children: map[string]*treeNode{}}
				}
				node = node.children[k]
			}
		}
	}
	return root
}

func marshalTree(node *treeNode) ([]byte, error) {
	if node.children == nil {
		return node.value, nil
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range node.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		buf.Write(kb)
		buf.WriteByte(':')
		childData, err := marshalTree(node.children[k])
		if err != nil {
			return nil, err
		}
		buf.Write(childData)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// --- public API -------------------------------------------------------------

// newKeysChunks compares the local source file against what Accent currently
// has (existingData) at the leaf level and produces a set of temp files each
// containing at most chunkSize new leaf keys, reconstructed as proper nested
// JSON objects.
//
// If existingData is nil (document not yet in Accent) all keys are treated as
// new. Returns nil if the file is not a JSON object or has no new keys.
// The caller must delete the returned temp files after use.
// newKeysChunksWithLeaves is like newKeysChunks but also returns the new leaf
// entries so callers can use them for targeted translation uploads.
func newKeysChunksWithLeaves(localPath string, existingData []byte, chunkSize int) (paths []string, newLeaves []leafEntry, err error) {
	localData, err := os.ReadFile(localPath)
	if err != nil {
		return nil, nil, err
	}
	localObj, err := parseJSONObject(localData)
	if err != nil || localObj == nil {
		return nil, nil, err
	}

	existingSet := map[string]bool{}
	var existingLeaves []leafEntry
	if len(existingData) > 0 {
		accObj, err := parseJSONObject(existingData)
		if err == nil && accObj != nil {
			existingLeaves = collectLeaves(accObj, nil)
			for _, leaf := range existingLeaves {
				existingSet[leafKey(leaf.path)] = true
			}
		}
	}

	allLeaves := collectLeaves(localObj, nil)
	for _, l := range allLeaves {
		if !existingSet[leafKey(l.path)] {
			newLeaves = append(newLeaves, l)
		}
	}

	if len(newLeaves) == 0 {
		return nil, nil, nil
	}

	for i := 0; i < len(newLeaves); i += chunkSize {
		end := i + chunkSize
		if end > len(newLeaves) {
			end = len(newLeaves)
		}
		combined := make([]leafEntry, 0, len(existingLeaves)+end)
		combined = append(combined, existingLeaves...)
		combined = append(combined, newLeaves[:end]...)

		data, err := marshalTree(buildTree(combined))
		if err != nil {
			return nil, nil, err
		}
		tmp, err := os.CreateTemp("", "accentctl-chunk-*.json")
		if err != nil {
			return nil, nil, err
		}
		if _, err := tmp.Write(data); err != nil {
			tmp.Close()
			return nil, nil, err
		}
		tmp.Close()
		paths = append(paths, tmp.Name())
	}
	return paths, newLeaves, nil
}

func newKeysChunks(localPath string, existingData []byte, chunkSize int) (paths []string, newCount int, err error) {
	localData, err := os.ReadFile(localPath)
	if err != nil {
		return nil, 0, err
	}

	localObj, err := parseJSONObject(localData)
	if err != nil || localObj == nil {
		return nil, 0, err
	}

	// Parse existing Accent state once: build both a lookup set and a leaf
	// slice. The set is used to find new keys; the slice is included in every
	// chunk so Accent always sees a complete file and never drops already-synced keys.
	existingSet := map[string]bool{}
	var existingLeaves []leafEntry
	if len(existingData) > 0 {
		accObj, err := parseJSONObject(existingData)
		if err == nil && accObj != nil {
			existingLeaves = collectLeaves(accObj, nil)
			for _, leaf := range existingLeaves {
				existingSet[leafKey(leaf.path)] = true
			}
		}
	}

	// Collect new leaves preserving source order.
	allLeaves := collectLeaves(localObj, nil)
	var newLeaves []leafEntry
	for _, l := range allLeaves {
		if !existingSet[leafKey(l.path)] {
			newLeaves = append(newLeaves, l)
		}
	}

	if len(newLeaves) == 0 {
		return nil, 0, nil
	}

	// Each chunk contains: existing Accent leaves + ALL new leaves uploaded so
	// far (previous batches) + this batch. This ensures every upload is a
	// cumulative superset of the previous one so no previously-uploaded key is
	// ever absent from a later chunk file.
	for i := 0; i < len(newLeaves); i += chunkSize {
		end := i + chunkSize
		if end > len(newLeaves) {
			end = len(newLeaves)
		}

		// existingLeaves + all new keys up to and including this batch
		combined := make([]leafEntry, 0, len(existingLeaves)+end)
		combined = append(combined, existingLeaves...)
		combined = append(combined, newLeaves[:end]...)

		data, err := marshalTree(buildTree(combined))
		if err != nil {
			return nil, 0, err
		}

		tmp, err := os.CreateTemp("", "accentctl-chunk-*.json")
		if err != nil {
			return nil, 0, err
		}
		if _, err := tmp.Write(data); err != nil {
			tmp.Close()
			return nil, 0, err
		}
		tmp.Close()
		paths = append(paths, tmp.Name())
	}
	return paths, len(newLeaves), nil
}
