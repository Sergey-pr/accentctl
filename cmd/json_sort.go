package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"sort"
)

// sortJSONFile reads a JSON file, sorts all object keys recursively, and writes it back.
// Works for both flat ({"a:b": "v"}) and nested ({"a": {"b": "v"}}) JSON.
func sortJSONFile(filePath string, desc bool) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	var raw json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil // not valid JSON, skip silently
	}

	sorted, err := sortRawJSON(raw, desc)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	if err := json.Indent(&buf, sorted, "", "  "); err != nil {
		return err
	}
	buf.WriteByte('\n')

	return os.WriteFile(filePath, buf.Bytes(), 0o644)
}

func sortRawJSON(raw json.RawMessage, desc bool) (json.RawMessage, error) {
	// Try to parse as object
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err == nil {
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if desc {
			sort.Sort(sort.Reverse(sort.StringSlice(keys)))
		}

		var buf bytes.Buffer
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			keyBytes, _ := json.Marshal(k)
			buf.Write(keyBytes)
			buf.WriteByte(':')
			val, err := sortRawJSON(obj[k], desc)
			if err != nil {
				return nil, err
			}
			buf.Write(val)
		}
		buf.WriteByte('}')
		return buf.Bytes(), nil
	}

	// Try array — recurse into elements
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err == nil {
		elems := make([]json.RawMessage, len(arr))
		for i, elem := range arr {
			sorted, err := sortRawJSON(elem, desc)
			if err != nil {
				return nil, err
			}
			elems[i] = sorted
		}
		out, err := json.Marshal(elems)
		return out, err
	}

	// Primitive — return as-is
	return raw, nil
}
