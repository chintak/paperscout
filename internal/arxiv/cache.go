package arxiv

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	cacheEnvVar        = "PAPERSCOUT_CACHE_DIR"
	cacheSubdir        = "paperscout/pdfs"
	cacheTTL           = 24 * time.Hour
	partialSuffix      = ".part"
	metaSuffix         = ".meta"
	defaultHTTPTimeout = 90 * time.Second
)

type pdfCache struct {
	dir    string
	client *http.Client
}

type pdfCacheMeta struct {
	URL          string    `json:"url"`
	ETag         string    `json:"etag"`
	LastModified string    `json:"lastModified"`
	CachedAt     time.Time `json:"cachedAt"`
	Size         int64     `json:"size"`
}

func newPDFCache(client *http.Client) (*pdfCache, error) {
	dir := os.Getenv(cacheEnvVar)
	if dir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			base = filepath.Join(os.TempDir(), "paperscout-cache")
		}
		dir = filepath.Join(base, cacheSubdir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &pdfCache{dir: dir, client: client}, nil
}

func (c *pdfCache) Fetch(ctx context.Context, pdfURL string) (string, error) {
	key := cacheKey(pdfURL)
	pdfPath, metaPath, partialPath := c.pathsFor(key)

	if info, err := os.Stat(pdfPath); err == nil && time.Since(info.ModTime()) < cacheTTL && info.Size() > 0 {
		return pdfPath, nil
	}

	meta, _ := readMeta(metaPath)
	info, _ := os.Stat(pdfPath)
	path, err := c.download(ctx, pdfURL, pdfPath, metaPath, partialPath, meta, info)
	if err == nil {
		return path, nil
	}
	if info != nil && info.Size() > 0 {
		return pdfPath, nil
	}
	return "", err
}

func (c *pdfCache) download(ctx context.Context, pdfURL, pdfPath, metaPath, partialPath string, meta pdfCacheMeta, current os.FileInfo) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pdfURL, nil)
	if err != nil {
		return "", err
	}
	if current != nil && current.Size() > 0 {
		if meta.ETag != "" {
			req.Header.Set("If-None-Match", meta.ETag)
		}
		if meta.LastModified != "" {
			req.Header.Set("If-Modified-Since", meta.LastModified)
		}
	}

	var partialSize int64
	if info, err := os.Stat(partialPath); err == nil && info.Size() > 0 {
		partialSize = info.Size()
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", partialSize))
		if meta.ETag != "" {
			req.Header.Set("If-Range", meta.ETag)
		} else if meta.LastModified != "" {
			req.Header.Set("If-Range", meta.LastModified)
		}
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotModified:
		if current != nil && current.Size() > 0 {
			meta.CachedAt = time.Now().UTC()
			writeMeta(metaPath, meta)
			return pdfPath, nil
		}
		return c.download(ctx, pdfURL, pdfPath, metaPath, partialPath, pdfCacheMeta{}, nil)
	case http.StatusOK:
		return c.saveBody(resp, pdfPath, metaPath, partialPath, false)
	case http.StatusPartialContent:
		appendExisting := partialSize > 0
		return c.saveBody(resp, pdfPath, metaPath, partialPath, appendExisting)
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("pdf download failed: %s (%s)", resp.Status, string(body))
	}
}

func (c *pdfCache) saveBody(resp *http.Response, pdfPath, metaPath, partialPath string, appendExisting bool) (string, error) {
	flags := os.O_CREATE | os.O_WRONLY
	if appendExisting {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	file, err := os.OpenFile(partialPath, flags, 0o644)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(file, resp.Body); err != nil {
		file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(partialPath, pdfPath); err != nil {
		return "", err
	}

	meta := pdfCacheMeta{
		URL:          resp.Request.URL.String(),
		ETag:         resp.Header.Get("Etag"),
		LastModified: resp.Header.Get("Last-Modified"),
		CachedAt:     time.Now().UTC(),
	}
	if info, err := os.Stat(pdfPath); err == nil {
		meta.Size = info.Size()
	}
	if err := writeMeta(metaPath, meta); err != nil {
		return "", err
	}
	return pdfPath, nil
}

func (c *pdfCache) pathsFor(key string) (string, string, string) {
	return filepath.Join(c.dir, key+".pdf"), filepath.Join(c.dir, key+metaSuffix), filepath.Join(c.dir, key+partialSuffix)
}

func cacheKey(pdfURL string) string {
	if id := extractIdentifier(pdfURL); id != "" {
		return sanitizeKey(id)
	}
	sum := sha1.Sum([]byte(pdfURL))
	return hex.EncodeToString(sum[:])
}

func sanitizeKey(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "/", "-")
	value = strings.ReplaceAll(value, ":", "-")
	value = strings.ReplaceAll(value, "..", "-")
	return value
}

func readMeta(path string) (pdfCacheMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return pdfCacheMeta{}, err
	}
	var meta pdfCacheMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return pdfCacheMeta{}, err
	}
	return meta, nil
}

func writeMeta(path string, meta pdfCacheMeta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
