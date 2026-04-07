package helpers

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
)

// LeafEntry is a single leaf key path and its raw JSON value.
type LeafEntry struct {
	Path  []string
	Value json.RawMessage
}

// LeafKey returns a map-safe string for a path.
// Uses \x00 as separator invalid in JSON string content.
func LeafKey(path []string) string { return strings.Join(path, "\x00") }

// CollectLeaves recursively gathers all leaf (non-object) entries from obj.
func CollectLeaves(obj *JSONObject, prefix []string) []LeafEntry {
	var out []LeafEntry
	for _, k := range obj.Keys {
		path := append(append([]string{}, prefix...), k)
		child, _ := ParseJSONObject(obj.Values[k])
		if child != nil {
			out = append(out, CollectLeaves(child, path)...)
		} else {
			out = append(out, LeafEntry{Path: path, Value: obj.Values[k]})
		}
	}
	return out
}

type treeNode struct {
	keys     []string
	children map[string]*treeNode
	value    json.RawMessage
}

func buildTree(leaves []LeafEntry) *treeNode {
	root := &treeNode{children: map[string]*treeNode{}}
	for _, leaf := range leaves {
		node := root
		for i, k := range leaf.Path {
			if i == len(leaf.Path)-1 {
				node.keys = append(node.keys, k)
				node.children[k] = &treeNode{value: leaf.Value}
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

// MarshalLeaves builds a nested JSON object from a slice of leaf entries.
func MarshalLeaves(leaves []LeafEntry) ([]byte, error) {
	return marshalTree(buildTree(leaves))
}

// NewKeysChunksWithLeaves compares the local source file against existingData
// at the leaf level, produces temp files of at most chunkSize new leaf keys,
// and returns both the file paths and the new leaf entries.
func NewKeysChunksWithLeaves(localPath string, existingData []byte, chunkSize int) (paths []string, newLeaves []LeafEntry, err error) {
	localData, err := os.ReadFile(localPath)
	if err != nil {
		return nil, nil, err
	}
	localObj, err := ParseJSONObject(localData)
	if err != nil || localObj == nil {
		return nil, nil, err
	}

	existingSet := map[string]bool{}
	var existingLeaves []LeafEntry
	if len(existingData) > 0 {
		accObj, err := ParseJSONObject(existingData)
		if err == nil && accObj != nil {
			existingLeaves = CollectLeaves(accObj, nil)
			for _, leaf := range existingLeaves {
				existingSet[LeafKey(leaf.Path)] = true
			}
		}
	}

	allLeaves := CollectLeaves(localObj, nil)
	for _, l := range allLeaves {
		if !existingSet[LeafKey(l.Path)] {
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
		combined := make([]LeafEntry, 0, len(existingLeaves)+end)
		combined = append(combined, existingLeaves...)
		combined = append(combined, newLeaves[:end]...)

		data, err := MarshalLeaves(combined)
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

// NewKeysChunks is like NewKeysChunksWithLeaves but returns only the count of
// new keys instead of the leaf entries.
func NewKeysChunks(localPath string, existingData []byte, chunkSize int) (paths []string, newCount int, err error) {
	paths, newLeaves, err := NewKeysChunksWithLeaves(localPath, existingData, chunkSize)
	return paths, len(newLeaves), err
}
