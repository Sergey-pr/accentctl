package helpers

import (
	"encoding/json"
	"os"
	"testing"
)

func runSortJSONFile(t *testing.T, input string, desc bool) []byte {
	t.Helper()
	path := writeTempJSON(t, input)
	if err := SortJSONFile(path, desc); err != nil {
		t.Fatalf("SortJSONFile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSortJSONFile_ascendingFlat(t *testing.T) {
	out := runSortJSONFile(t, `{"z":"1","a":"2","m":"3"}`, false)

	var result map[string]json.RawMessage
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatal(err)
	}

	// Verify all keys present with correct values.
	if string(result["a"]) != `"2"` || string(result["m"]) != `"3"` || string(result["z"]) != `"1"` {
		t.Errorf("unexpected values: %s", out)
	}

	// Verify key order in raw output.
	raw := string(out)
	posA := indexOf(raw, `"a"`)
	posM := indexOf(raw, `"m"`)
	posZ := indexOf(raw, `"z"`)
	if !(posA < posM && posM < posZ) {
		t.Errorf("keys not in ascending order in output:\n%s", out)
	}
}

func TestSortJSONFile_descendingFlat(t *testing.T) {
	out := runSortJSONFile(t, `{"a":"1","m":"2","z":"3"}`, true)

	raw := string(out)
	posA := indexOf(raw, `"a"`)
	posM := indexOf(raw, `"m"`)
	posZ := indexOf(raw, `"z"`)
	if !(posZ < posM && posM < posA) {
		t.Errorf("keys not in descending order in output:\n%s", out)
	}
}

func TestSortJSONFile_nested(t *testing.T) {
	out := runSortJSONFile(t, `{"b":{"z":"1","a":"2"},"a":"3"}`, false)

	raw := string(out)
	// Top-level: "a" before "b"
	posTopA := indexOf(raw, `"a": "3"`)
	posTopB := indexOf(raw, `"b"`)
	if !(posTopA < posTopB) {
		t.Errorf("top-level keys not sorted:\n%s", out)
	}
	// Nested: "a" before "z"
	posNestedA := indexOf(raw, `"a": "2"`)
	posNestedZ := indexOf(raw, `"z"`)
	if !(posNestedA < posNestedZ) {
		t.Errorf("nested keys not sorted:\n%s", out)
	}
}

func TestSortJSONFile_alreadySorted(t *testing.T) {
	input := `{"a":"1","b":"2","c":"3"}`
	out := runSortJSONFile(t, input, false)
	jsonEqual(t, out, input)
}

func TestSortJSONFile_singleKey(t *testing.T) {
	out := runSortJSONFile(t, `{"only":"value"}`, false)
	jsonEqual(t, out, `{"only":"value"}`)
}

func TestSortJSONFile_emptyObject(t *testing.T) {
	out := runSortJSONFile(t, `{}`, false)
	jsonEqual(t, out, `{}`)
}

func TestSortJSONFile_invalidJSONSkipped(t *testing.T) {
	// Invalid JSON should be silently skipped (file left unchanged).
	path := writeTempJSON(t, `not json`)
	original, _ := os.ReadFile(path)
	if err := SortJSONFile(path, false); err != nil {
		t.Fatalf("expected no error for invalid JSON, got: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(original) {
		t.Errorf("file should be unchanged for invalid JSON")
	}
}

func TestSortJSONFile_writesIndented(t *testing.T) {
	out := runSortJSONFile(t, `{"a":"1","b":"2"}`, false)
	// Output should be indented (contains newlines).
	if !containsNewline(string(out)) {
		t.Errorf("expected indented output, got: %s", out)
	}
}

func indexOf(s, substr string) int {
	for i := range s {
		if len(s[i:]) >= len(substr) && s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func containsNewline(s string) bool {
	for _, c := range s {
		if c == '\n' {
			return true
		}
	}
	return false
}
