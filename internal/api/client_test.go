package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/sergey-pr/accentctl/internal/constants"
)

func init() {
	constants.RequestDelay = 0
}

func newTestServer(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(srv.URL, "key", false)
}

func writeTempJSON(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "file.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSyncDecodesPeekResult(t *testing.T) {
	var gotAuth, gotSyncType string
	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		gotSyncType = r.FormValue("sync_type")
		_, _ = w.Write([]byte(`{"data":{"new_count":3,"removed_count":1}}`))
	})

	result, err := client.Sync(writeTempJSON(t, `{"a":"A"}`), "app", "json", "en", SyncOptions{SyncType: "smart"})
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer key" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer key")
	}
	if gotSyncType != "smart" {
		t.Errorf("sync_type = %q, want %q", gotSyncType, "smart")
	}
	if result.NewCount != 3 || result.RemovedCount != 1 {
		t.Errorf("result = %+v, want new 3, removed 1", result)
	}
}

func TestAddTranslationsNotFound(t *testing.T) {
	client := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	_, err := client.AddTranslations(writeTempJSON(t, `{}`), "app", "json", "fr", AddTranslationsOptions{MergeType: "force"})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestPostOperationServerError(t *testing.T) {
	client := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	})
	_, err := client.Sync(writeTempJSON(t, `{}`), "app", "json", "en", SyncOptions{})
	if err == nil {
		t.Fatal("want error on HTTP 400, got nil")
	}
}

func TestExportBytes(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		want    string
		wantErr bool
	}{
		{"ok", http.StatusOK, `{"a":"A"}`, `{"a":"A"}`, false},
		{"not found returns nil, nil", http.StatusNotFound, "", "", false},
		{"server error", http.StatusInternalServerError, "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			})
			got, err := client.ExportBytes("app", "json", "en")
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if string(got) != tt.want {
				t.Errorf("body = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExportWritesFileAndCreatesDirs(t *testing.T) {
	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("order_by") != "key" {
			t.Errorf("order_by = %q, want %q", r.URL.Query().Get("order_by"), "key")
		}
		_, _ = w.Write([]byte(`{"a":"A"}`))
	})

	dest := filepath.Join(t.TempDir(), "nested", "dir", "app.json")
	if err := client.Export(dest, "app", "json", "en", ExportOptions{OrderBy: "key"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"a":"A"}` {
		t.Errorf("file content = %q, want %q", data, `{"a":"A"}`)
	}
}

func TestExportNotFound(t *testing.T) {
	client := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	err := client.Export(filepath.Join(t.TempDir(), "app.json"), "app", "json", "en", ExportOptions{})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
