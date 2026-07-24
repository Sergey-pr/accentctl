package api

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrNotFound is returned when the API responds with HTTP 404.
var ErrNotFound = fmt.Errorf("not found")

type Client struct {
	apiURL string
	apiKey string
	http   *http.Client
}

func New(apiURL, apiKey string, verbose bool, delay time.Duration) *Client {
	var transport http.RoundTripper = &throttledTransport{wrapped: http.DefaultTransport, delay: delay}
	if verbose {
		transport = &verboseTransport{wrapped: transport}
	}
	return &Client{
		apiURL: strings.TrimRight(apiURL, "/"),
		apiKey: apiKey,
		http:   &http.Client{Timeout: 60 * time.Second, Transport: transport},
	}
}

type throttledTransport struct {
	wrapped http.RoundTripper
	delay   time.Duration
}

func (t *throttledTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.wrapped.RoundTrip(req)
	if t.delay > 0 {
		time.Sleep(t.delay)
	}
	return resp, err
}

type verboseTransport struct {
	wrapped http.RoundTripper
}

func (t *verboseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	fmt.Printf("[verbose] %s %s\n", req.Method, req.URL)
	resp, err := t.wrapped.RoundTrip(req)
	if err != nil {
		fmt.Printf("[verbose] error: %v\n", err)
		return nil, err
	}
	fmt.Printf("[verbose] -> %s\n", resp.Status)
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		fmt.Printf("[verbose] body: %s\n", body)
		resp.Body = io.NopCloser(bytes.NewReader(body))
	}
	return resp, nil
}

// SyncOptions controls the sync operation.
type SyncOptions struct {
	SyncType string // smart | passive
	OrderBy  string // index | key-asc
}

// AddTranslationsOptions controls the add-translations operation.
type AddTranslationsOptions struct {
	MergeType string // smart | passive | force
}

// ExportOptions controls the export operation.
type ExportOptions struct {
	OrderBy string // index | key-asc
}

// Sync uploads a file and syncs it with Accent.
func (c *Client) Sync(filePath, documentPath, format, language string, opts SyncOptions) error {
	endpoint := c.apiURL + "/sync"

	body, contentType, err := buildMultipart(func(w *multipart.Writer) error {
		if err := writeFile(w, "file", filePath); err != nil {
			return err
		}
		_ = w.WriteField("document_path", documentPath)
		_ = w.WriteField("document_format", format)
		if language != "" {
			_ = w.WriteField("language", language)
		}
		if opts.SyncType != "" {
			_ = w.WriteField("sync_type", opts.SyncType)
		}
		return nil
	})
	if err != nil {
		return err
	}

	return c.postOperation(endpoint, body, contentType)
}

// AddTranslations uploads a translation file to Accent.
func (c *Client) AddTranslations(filePath, documentPath, format, language string, opts AddTranslationsOptions) error {
	endpoint := c.apiURL + "/add-translations"
	body, contentType, err := buildMultipart(func(w *multipart.Writer) error {
		if err := writeFile(w, "file", filePath); err != nil {
			return err
		}
		_ = w.WriteField("document_path", documentPath)
		_ = w.WriteField("document_format", format)
		_ = w.WriteField("language", language)
		if opts.MergeType != "" {
			_ = w.WriteField("merge_type", opts.MergeType)
		}
		return nil
	})
	if err != nil {
		return err
	}

	return c.postOperation(endpoint, body, contentType)
}

// exportRequest performs GET /export and returns the raw response.
func (c *Client) exportRequest(documentPath, format, language, orderBy string) (*http.Response, error) {
	q := url.Values{}
	q.Set("document_path", documentPath)
	q.Set("document_format", format)
	q.Set("language", language)
	if orderBy != "" {
		q.Set("order_by", orderBy)
	}

	req, err := http.NewRequest(http.MethodGet, c.apiURL+"/export?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	c.setAuth(req)
	return c.http.Do(req)
}

// ExportBytes fetches a translated file from Accent and returns its raw contents.
// Returns ErrNotFound when the document/language does not exist (HTTP 404).
func (c *Client) ExportBytes(documentPath, format, language string) ([]byte, error) {
	resp, err := c.exportRequest(documentPath, format, language, "")
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode >= 400 {
		return nil, httpError("export failed", resp)
	}
	return io.ReadAll(resp.Body)
}

// Export downloads a translated file from Accent and writes it to destPath.
func (c *Client) Export(destPath, documentPath, format, language string, opts ExportOptions) error {
	resp, err := c.exportRequest(documentPath, format, language, opts.OrderBy)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode >= 400 {
		return httpError("export failed", resp)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(destPath, resp.Body)
}

// writeFileAtomic streams r into a same-directory temp file and renames it over
// destPath, so a failed download never truncates the existing file.
func writeFileAtomic(destPath string, r io.Reader) error {
	tmp, err := os.CreateTemp(filepath.Dir(destPath), ".accentctl-*")
	if err != nil {
		return err
	}
	if _, err := io.Copy(tmp, r); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	// CreateTemp files are 0600; pulled files should stay world-readable.
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), destPath); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

func (c *Client) setAuth(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
}

func httpError(op string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if msg := strings.TrimSpace(string(body)); msg != "" {
		return fmt.Errorf("%s: HTTP %d: %s", op, resp.StatusCode, msg)
	}
	return fmt.Errorf("%s: HTTP %d", op, resp.StatusCode)
}

// postOperation sends the request and checks the status; the Accent server
// answers successful sync/merge calls with an empty body, so there is nothing to decode.
func (c *Client) postOperation(endpoint string, body *bytes.Buffer, contentType string) error {
	req, err := http.NewRequest(http.MethodPost, endpoint, body)
	if err != nil {
		return err
	}
	c.setAuth(req)
	req.Header.Set("Content-Type", contentType)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode >= 400 {
		return httpError("API error", resp)
	}
	return nil
}

func buildMultipart(fn func(*multipart.Writer) error) (*bytes.Buffer, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := fn(w); err != nil {
		return nil, "", err
	}
	_ = w.Close()
	return &buf, w.FormDataContentType(), nil
}

func writeFile(w *multipart.Writer, field, filePath string) error {
	fw, err := w.CreateFormFile(field, filepath.Base(filePath))
	if err != nil {
		return err
	}
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}

	defer func() {
		_ = f.Close()
	}()

	_, err = io.Copy(fw, f)
	return err
}
