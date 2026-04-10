package helpers

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
)

// NodeEntry is a single node path and its raw JSON value.
type NodeEntry struct {
	Path  []string
	Value json.RawMessage
}

// NodeKey returns a map-safe string for a path.
// Uses \x00 as separator invalid in JSON string content.
func NodeKey(path []string) string { return strings.Join(path, "\x00") }

// CollectNodes recursively gathers all non-object entries from obj.
func CollectNodes(obj *JSONObject, prefix []string) []NodeEntry {
	var out []NodeEntry
	for _, k := range obj.Keys {
		path := append(append([]string{}, prefix...), k)
		child, _ := ParseJSONObject(obj.Values[k])
		if child != nil {
			out = append(out, CollectNodes(child, path)...)
		} else {
			out = append(out, NodeEntry{Path: path, Value: obj.Values[k]})
		}
	}
	return out
}

type treeNode struct {
	keys     []string
	children map[string]*treeNode
	value    json.RawMessage
}

func buildTree(nodes []NodeEntry) *treeNode {
	root := &treeNode{children: map[string]*treeNode{}}
	for _, n := range nodes {
		node := root
		for i, k := range n.Path {
			if i == len(n.Path)-1 {
				node.keys = append(node.keys, k)
				node.children[k] = &treeNode{value: n.Value}
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

// MarshalNodes builds a nested JSON object from a slice of node entries.
func MarshalNodes(nodes []NodeEntry) ([]byte, error) {
	return marshalTree(buildTree(nodes))
}

// NewKeysChunksWithNodes compares the local source file against existingData,
// produces temp files of at most chunkSize new keys,
// and returns both the file paths and the new node entries.
func NewKeysChunksWithNodes(localPath string, existingData []byte, chunkSize int) (paths []string, newNodes []NodeEntry, err error) {
	localData, err := os.ReadFile(localPath)
	if err != nil {
		return nil, nil, err
	}
	localObj, err := ParseJSONObject(localData)
	if err != nil || localObj == nil {
		return nil, nil, err
	}

	existingSet := map[string]bool{}
	var existingNodes []NodeEntry
	if len(existingData) > 0 {
		accentObj, err := ParseJSONObject(existingData)
		if err == nil && accentObj != nil {
			existingNodes = CollectNodes(accentObj, nil)
			for _, n := range existingNodes {
				existingSet[NodeKey(n.Path)] = true
			}
		}
	}

	allNodes := CollectNodes(localObj, nil)
	for _, l := range allNodes {
		if !existingSet[NodeKey(l.Path)] {
			newNodes = append(newNodes, l)
		}
	}

	if len(newNodes) == 0 {
		return nil, nil, nil
	}

	for i := 0; i < len(newNodes); i += chunkSize {
		end := i + chunkSize
		if end > len(newNodes) {
			end = len(newNodes)
		}
		combined := make([]NodeEntry, 0, len(existingNodes)+end)
		combined = append(combined, existingNodes...)
		combined = append(combined, newNodes[:end]...)

		data, err := MarshalNodes(combined)
		if err != nil {
			return nil, nil, err
		}
		tmp, err := os.CreateTemp("", "accentctl-chunk-*.json")
		if err != nil {
			return nil, nil, err
		}
		if _, err := tmp.Write(data); err != nil {
			_ = tmp.Close()
			return nil, nil, err
		}
		_ = tmp.Close()
		paths = append(paths, tmp.Name())
	}
	return paths, newNodes, nil
}

// NewKeysChunks is like NewKeysChunksWithNodes but returns only the count of
// new keys instead of the node entries.
func NewKeysChunks(localPath string, existingData []byte, chunkSize int) (paths []string, newCount int, err error) {
	paths, newNodes, err := NewKeysChunksWithNodes(localPath, existingData, chunkSize)
	return paths, len(newNodes), err
}
