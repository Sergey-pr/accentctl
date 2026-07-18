package helpers

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
)

// SortJSONFile reads a JSON file, sorts all object keys recursively, and writes it back.
func SortJSONFile(filePath string, desc bool) error {
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

	tmp, err := os.CreateTemp(filepath.Dir(filePath), ".accentctl-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), filePath); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

func sortRawJSON(raw json.RawMessage, desc bool) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err == nil {
		keys := slices.Sorted(maps.Keys(obj))
		if desc {
			slices.Reverse(keys)
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

	return raw, nil
}
