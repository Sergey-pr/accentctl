package helpers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

// JSONObject holds top-level keys in insertion order plus their raw values.
type JSONObject struct {
	Keys   []string
	Values map[string]json.RawMessage
}

// ReadJSONObjectFile reads a localization file and parses it as a JSON object.
//
// Unlike ParseJSONObject it treats "valid JSON, but not an object" as an error:
// a top-level array or string is a broken localization file, and every caller
// that reads one from disk wants to say so rather than quietly process no keys.
func ReadJSONObjectFile(path string) (*JSONObject, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	obj, err := ParseJSONObject(data)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid JSON: %w", path, err)
	}
	if obj == nil {
		return nil, fmt.Errorf("%s: not a JSON object (a localization file must have an object at the top level)", path)
	}
	return obj, nil
}

// ParseJSONObject parses a JSON object preserving insertion order.
//
// A nil object with a nil error means the data is valid JSON that is not an
// object. CollectNodes relies on that signal to tell a nested object from a
// leaf value, so it must not become an error here; callers reading whole files
// from disk should use ReadJSONObjectFile instead.
func ParseJSONObject(data []byte) (*JSONObject, error) {
	dec := json.NewDecoder(bytes.NewReader(data))

	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, nil // not an object
	}

	obj := &JSONObject{Values: map[string]json.RawMessage{}}
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
		obj.Keys = append(obj.Keys, key)
		obj.Values[key] = raw
	}
	return obj, nil
}
