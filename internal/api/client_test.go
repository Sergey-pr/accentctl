package api

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func newTestServer(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(srv.URL, "key", false, 0)
}

func writeTempJSON(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "file.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// The real Accent server answers a successful sync with 200 and an empty body.
func TestSyncAcceptsEmptyResponseBody(t *testing.T) {
	var gotAuth, gotSyncType string
	client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		gotSyncType = r.FormValue("sync_type")
		w.WriteHeader(http.StatusOK)
	})

	err := client.Sync(writeTempJSON(t, `{"a":"A"}`), "app", "json", "en", SyncOptions{SyncType: "smart"})
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer key" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer key")
	}
	if gotSyncType != "smart" {
		t.Errorf("sync_type = %q, want %q", gotSyncType, "smart")
	}
}

func TestUploadsSendExpectedRequest(t *testing.T) {
	tests := []struct {
		name       string
		call       func(c *Client, file string) error
		wantPath   string
		wantFields map[string][]string
	}{
		{
			name: "sync",
			call: func(c *Client, file string) error {
				return c.Sync(file, "app", "json", "en", SyncOptions{SyncType: "passive"})
			},
			wantPath:   "/sync",
			wantFields: map[string][]string{"document_path": {"app"}, "document_format": {"json"}, "language": {"en"}, "sync_type": {"passive"}},
		},
		{
			name: "sync omits empty language and sync_type",
			call: func(c *Client, file string) error {
				return c.Sync(file, "app", "json", "", SyncOptions{})
			},
			wantPath:   "/sync",
			wantFields: map[string][]string{"document_path": {"app"}, "document_format": {"json"}},
		},
		{
			name: "add-translations",
			call: func(c *Client, file string) error {
				return c.AddTranslations(file, "app", "json", "fr", AddTranslationsOptions{MergeType: "force"})
			},
			wantPath:   "/add-translations",
			wantFields: map[string][]string{"document_path": {"app"}, "document_format": {"json"}, "language": {"fr"}, "merge_type": {"force"}},
		},
		{
			name: "add-translations sends empty language but omits empty merge_type",
			call: func(c *Client, file string) error {
				return c.AddTranslations(file, "app", "json", "", AddTranslationsOptions{})
			},
			wantPath:   "/add-translations",
			wantFields: map[string][]string{"document_path": {"app"}, "document_format": {"json"}, "language": {""}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var method, path, fileName, fileBody string
			var fields map[string][]string
			client := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Error(err)
					return
				}
				method, path, fields = r.Method, r.URL.Path, r.MultipartForm.Value
				f, hdr, err := r.FormFile("file")
				if err != nil {
					t.Error(err)
					return
				}
				defer func() {
					_ = f.Close()
				}()
				body, _ := io.ReadAll(f)
				fileName, fileBody = hdr.Filename, string(body)
			})

			if err := tt.call(client, writeTempJSON(t, `{"a":"A"}`)); err != nil {
				t.Fatal(err)
			}
			if method != http.MethodPost || path != tt.wantPath {
				t.Errorf("request = %s %s, want POST %s", method, path, tt.wantPath)
			}
			if fileName != "file.json" || fileBody != `{"a":"A"}` {
				t.Errorf("file = %q %q, want \"file.json\" with the local contents", fileName, fileBody)
			}
			if !reflect.DeepEqual(fields, tt.wantFields) {
				t.Errorf("fields = %v, want %v", fields, tt.wantFields)
			}
		})
	}
}

func TestUploadWithMissingFileSendsNoRequest(t *testing.T) {
	called := false
	client := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		called = true
	})
	missing := filepath.Join(t.TempDir(), "missing.json")

	if err := client.Sync(missing, "app", "json", "en", SyncOptions{}); err == nil {
		t.Error("Sync with a missing file succeeded, want an error")
	}
	if err := client.AddTranslations(missing, "app", "json", "fr", AddTranslationsOptions{}); err == nil {
		t.Error("AddTranslations with a missing file succeeded, want an error")
	}
	if called {
		t.Error("a request reached the server, want none")
	}
}

func TestAddTranslationsNotFound(t *testing.T) {
	client := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	err := client.AddTranslations(writeTempJSON(t, `{}`), "app", "json", "fr", AddTranslationsOptions{MergeType: "force"})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestPostOperationServerError(t *testing.T) {
	client := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"invalid document_format"}`, http.StatusBadRequest)
	})
	err := client.Sync(writeTempJSON(t, `{}`), "app", "json", "en", SyncOptions{})
	if err == nil {
		t.Fatal("want error on HTTP 400, got nil")
	}
	if !strings.Contains(err.Error(), "HTTP 400") || !strings.Contains(err.Error(), "invalid document_format") {
		t.Errorf("err = %v, want status and response body included", err)
	}
}

func TestExportErrorsIncludeResponseBody(t *testing.T) {
	client := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "revision not configured", http.StatusInternalServerError)
	})

	if _, err := client.ExportBytes("app", "json", "en"); err == nil || !strings.Contains(err.Error(), "revision not configured") {
		t.Errorf("ExportBytes err = %v, want response body included", err)
	}

	err := client.Export(filepath.Join(t.TempDir(), "app.json"), "app", "json", "en", ExportOptions{})
	if err == nil || !strings.Contains(err.Error(), "revision not configured") {
		t.Errorf("Export err = %v, want response body included", err)
	}
}

func TestExportBytes(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		want       string
		wantErr    bool
		wantNotFnd bool
	}{
		{name: "ok", status: http.StatusOK, body: `{"a":"A"}`, want: `{"a":"A"}`},
		{name: "not found returns ErrNotFound", status: http.StatusNotFound, wantErr: true, wantNotFnd: true},
		{name: "server error", status: http.StatusInternalServerError, wantErr: true},
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
			if tt.wantNotFnd && !errors.Is(err, ErrNotFound) {
				t.Errorf("err = %v, want ErrNotFound", err)
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

// Export goes through a CreateTemp file (0600), which must not leak into the
// final file's permissions.
func TestExportWritesWorldReadableFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes are not meaningful on Windows")
	}
	client := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"a":"A"}`))
	})

	dest := filepath.Join(t.TempDir(), "app.json")
	if err := client.Export(dest, "app", "json", "en", ExportOptions{}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Errorf("file mode = %o, want 644", got)
	}
}

func TestExportMidStreamFailureKeepsExistingFile(t *testing.T) {
	client := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte(`{"partial`))
	})

	dir := t.TempDir()
	dest := filepath.Join(dir, "app.json")
	if err := os.WriteFile(dest, []byte(`{"a":"A"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := client.Export(dest, "app", "json", "en", ExportOptions{}); err == nil {
		t.Fatal("want error on mid-stream failure, got nil")
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"a":"A"}` {
		t.Errorf("file content = %q, want previous content %q", data, `{"a":"A"}`)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("dir has %d entries, want 1 (no leftover temp files): %v", len(entries), entries)
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
