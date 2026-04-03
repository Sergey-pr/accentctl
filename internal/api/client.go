package api

import (
	"bytes"
	"encoding/json"
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
// Callers can use errors.Is(err, api.ErrNotFound) to skip missing resources.
var ErrNotFound = fmt.Errorf("not found")

type Client struct {
	apiURL string
	apiKey string
	http   *http.Client
}

func New(apiURL, apiKey string, verbose bool) *Client {
	transport := http.DefaultTransport
	if verbose {
		transport = &verboseTransport{wrapped: transport}
	}
	return &Client{
		apiURL: strings.TrimRight(apiURL, "/"),
		apiKey: apiKey,
		http:   &http.Client{Timeout: 60 * time.Second, Transport: transport},
	}
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
	fmt.Printf("[verbose] → %s\n", resp.Status)
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Printf("[verbose] body: %s\n", body)
		resp.Body = io.NopCloser(bytes.NewReader(body))
	}
	return resp, nil
}

// PeekResult is the response body from a sync/add-translations peek call.
type PeekResult struct {
	NewCount       int `json:"new_count"`
	UpdatedCount   int `json:"updated_count"`
	RemovedCount   int `json:"removed_count"`
	ConflictsCount int `json:"conflicts_count"`
}

// SyncOptions controls the sync operation.
type SyncOptions struct {
	DryRun   bool
	SyncType string // smart | passive
	OrderBy  string // index | key-asc
}

// AddTranslationsOptions controls the add-translations operation.
type AddTranslationsOptions struct {
	DryRun    bool
	MergeType string // smart | passive | force
}

// ExportOptions controls the export operation.
type ExportOptions struct {
	OrderBy string // index | key-asc
}

// Sync uploads a file and syncs it with Accent.
// Returns peek stats when DryRun is true, nil otherwise.
func (c *Client) Sync(filePath, documentPath, format, language string, opts SyncOptions) (*PeekResult, error) {
	endpoint := c.apiURL + "/sync"
	if opts.DryRun {
		endpoint += "/peek"
	}

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
		return nil, err
	}

	return c.postOperation(endpoint, body, contentType, opts.DryRun)
}

// AddTranslations uploads a translation file to Accent.
func (c *Client) AddTranslations(filePath, documentPath, format, language string, opts AddTranslationsOptions) (*PeekResult, error) {
	endpoint := c.apiURL + "/add-translations"
	if opts.DryRun {
		endpoint += "/peek"
	}

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
		return nil, err
	}

	return c.postOperation(endpoint, body, contentType, opts.DryRun)
}

// ExportBytes fetches a translated file from Accent and returns its raw contents.
// Returns (nil, nil) when the document/language does not exist (HTTP 404).
func (c *Client) ExportBytes(documentPath, format, language string) ([]byte, error) {
	q := url.Values{}
	q.Set("document_path", documentPath)
	q.Set("document_format", format)
	q.Set("language", language)

	req, err := http.NewRequest(http.MethodGet, c.apiURL+"/export?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	c.setAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("export failed: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// Export downloads a translated file from Accent and writes it to destPath.
func (c *Client) Export(destPath, documentPath, format, language string, opts ExportOptions) error {
	q := url.Values{}
	q.Set("document_path", documentPath)
	q.Set("document_format", format)
	q.Set("language", language)
	if opts.OrderBy != "" {
		q.Set("order_by", opts.OrderBy)
	}

	req, err := http.NewRequest(http.MethodGet, c.apiURL+"/export?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	c.setAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("export failed: HTTP %d", resp.StatusCode)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}

	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

func (c *Client) setAuth(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
}

func (c *Client) postOperation(endpoint string, body *bytes.Buffer, contentType string, isDryRun bool) (*PeekResult, error) {
	req, err := http.NewRequest(http.MethodPost, endpoint, body)
	if err != nil {
		return nil, err
	}
	c.setAuth(req)
	req.Header.Set("Content-Type", contentType)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error: HTTP %d", resp.StatusCode)
	}

	if !isDryRun {
		return nil, nil
	}

	var result struct {
		Data PeekResult `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result.Data, nil
}

func buildMultipart(fn func(*multipart.Writer) error) (*bytes.Buffer, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := fn(w); err != nil {
		return nil, "", err
	}
	w.Close()
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
	defer f.Close()
	_, err = io.Copy(fw, f)
	return err
}
