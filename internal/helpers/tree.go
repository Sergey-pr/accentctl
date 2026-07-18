package helpers

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"strings"
)

// NodeEntry is a single node path and its raw JSON value.
type NodeEntry struct {
	Path  []string
	Value json.RawMessage
}

// NodeKey joins a path with \x00, which cannot appear in decoded JSON strings.
func NodeKey(path []string) string { return strings.Join(path, "\x00") }

// NodeSet returns the set of NodeKeys for the given nodes.
func NodeSet(nodes []NodeEntry) map[string]bool {
	set := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		set[NodeKey(n.Path)] = true
	}
	return set
}

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

// ServerNodes parses data exported from Accent, treating anything that is not
// a JSON object as an empty key set.
func ServerNodes(data []byte) []NodeEntry {
	if len(data) == 0 {
		return nil
	}
	obj, err := ParseJSONObject(data)
	if err != nil || obj == nil {
		return nil
	}
	return CollectNodes(obj, nil)
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
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
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

// WriteNodesTempFile marshals nodes into a temp JSON file and returns its path.
// The caller removes the file once it is uploaded.
func WriteNodesTempFile(nodes []NodeEntry, pattern string) (string, error) {
	data, err := MarshalNodes(nodes)
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

// NewKeysChunksWithNodes diffs the local file against server data and writes the
// new keys to cumulative chunk files: /sync drops keys absent from an upload.
func NewKeysChunksWithNodes(localPath string, existingData []byte, chunkSize int) (paths []string, newNodes []NodeEntry, err error) {
	localObj, err := ReadJSONObjectFile(localPath)
	if err != nil {
		return nil, nil, err
	}

	existingNodes := ServerNodes(existingData)
	existingSet := NodeSet(existingNodes)
	for _, n := range CollectNodes(localObj, nil) {
		if !existingSet[NodeKey(n.Path)] {
			newNodes = append(newNodes, n)
		}
	}
	if len(newNodes) == 0 {
		return nil, nil, nil
	}

	for start := 0; start < len(newNodes); start += chunkSize {
		end := min(start+chunkSize, len(newNodes))
		path, err := WriteNodesTempFile(slices.Concat(existingNodes, newNodes[:end]), "accentctl-chunk-*.json")
		if err != nil {
			return nil, nil, err
		}
		paths = append(paths, path)
	}
	return paths, newNodes, nil
}
