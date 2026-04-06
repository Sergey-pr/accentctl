package utils

import (
	"bytes"
	"encoding/json"
)

// JSONObject holds top-level keys in insertion order plus their raw values.
type JSONObject struct {
	Keys   []string
	Values map[string]json.RawMessage
}

// ParseJSONObject parses a JSON object preserving insertion order.
// Returns nil (no error) if the input is not a JSON object.
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
