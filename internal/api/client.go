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
	"time"
)

type Client struct {
	apiURL string
	apiKey string
	http   *http.Client
}

func New(apiURL, apiKey string) *Client {
	return &Client{
		apiURL: apiURL,
		apiKey: apiKey,
		http:   &http.Client{Timeout: 60 * time.Second},
	}
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
		_ = w.WriteField("language", language)
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

// ProjectStats holds stats for the project.
type ProjectStats struct {
	Project struct {
		Name           string  `json:"name"`
		TranslatedRate float64 `json:"translated_rate"`
		VersionsCount  int     `json:"versions_count"`
		DocumentsCount int     `json:"documents_count"`
	} `json:"project"`
	LanguageStats []struct {
		Language struct {
			Name string `json:"name"`
			Slug string `json:"slug"`
		} `json:"language"`
		TranslatedCount   int     `json:"translated_count"`
		UntranslatedCount int     `json:"untranslated_count"`
		TranslatedRate    float64 `json:"translated_rate"`
	} `json:"language_stats"`
}

// Stats fetches project stats from Accent.
func (c *Client) Stats() (*ProjectStats, error) {
	req, err := http.NewRequest(http.MethodGet, c.apiURL+"/stats", nil)
	if err != nil {
		return nil, err
	}
	c.setAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("stats failed: HTTP %d", resp.StatusCode)
	}

	var result struct {
		Data ProjectStats `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result.Data, nil
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
