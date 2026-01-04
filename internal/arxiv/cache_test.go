package arxiv

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPDFCacheReusesFreshFile(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv(cacheEnvVar, cacheDir)

	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Etag", `"v1"`)
		_, _ = w.Write([]byte("%PDF-1.4\nHello"))
	}))
	t.Cleanup(server.Close)

	cache, err := newPDFCache(server.Client())
	if err != nil {
		t.Fatalf("newPDFCache: %v", err)
	}
	ctx := context.Background()

	path, err := cache.Fetch(ctx, server.URL+"/pdf/2101.00001.pdf")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("cached file missing: %v", err)
	}
	if hits != 1 {
		t.Fatalf("expected single download, got %d hits", hits)
	}

	path2, err := cache.Fetch(ctx, server.URL+"/pdf/2101.00001.pdf")
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if path != path2 {
		t.Fatalf("paths differ: %s vs %s", path, path2)
	}
	if hits != 1 {
		t.Fatalf("cache miss triggered download, total hits %d", hits)
	}
}

func TestPDFCacheRespectsConditionalRefresh(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv(cacheEnvVar, cacheDir)

	var etag string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v2"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		etag = `"v2"`
		w.Header().Set("Etag", etag)
		_, _ = w.Write([]byte("%PDF-1.4\nUpdated"))
	}))
	t.Cleanup(server.Close)

	cache, err := newPDFCache(server.Client())
	if err != nil {
		t.Fatalf("newPDFCache: %v", err)
	}
	ctx := context.Background()

	path, err := cache.Fetch(ctx, server.URL+"/pdf/2201.00001.pdf")
	if err != nil {
		t.Fatalf("initial fetch: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("missing cached pdf: %v", err)
	}

	// Age the file to force a conditional request.
	old := time.Now().Add(-(cacheTTL + time.Hour))
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	if _, err := cache.Fetch(ctx, server.URL+"/pdf/2201.00001.pdf"); err != nil {
		t.Fatalf("conditional fetch: %v", err)
	}
	if etag == "" {
		t.Fatalf("expected server to be consulted for stale cache")
	}
}

func TestPDFCacheResumesPartialDownload(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv(cacheEnvVar, cacheDir)

	var rangeHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader = r.Header.Get("Range")
		w.Header().Set("Etag", `"resume"`)
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("world"))
	}))
	t.Cleanup(server.Close)

	cache, err := newPDFCache(server.Client())
	if err != nil {
		t.Fatalf("newPDFCache: %v", err)
	}
	ctx := context.Background()
	key := cacheKey(server.URL + "/pdf/2301.00001.pdf")
	pdfPath, metaPath, partPath := cache.pathsFor(key)

	if err := os.WriteFile(partPath, []byte("hello "), 0o644); err != nil {
		t.Fatalf("write partial: %v", err)
	}
	if err := writeMeta(metaPath, pdfCacheMeta{ETag: `"resume"`}); err != nil {
		t.Fatalf("write meta: %v", err)
	}

	path, err := cache.Fetch(ctx, server.URL+"/pdf/2301.00001.pdf")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if path != pdfPath {
		t.Fatalf("unexpected path: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read cached pdf: %v", err)
	}
	if string(data) != "hello world" {
		t.Fatalf("resume failed, got %q", string(data))
	}
	if rangeHeader != fmt.Sprintf("bytes=%d-", len("hello ")) {
		t.Fatalf("expected range header, got %q", rangeHeader)
	}
	if _, err := os.Stat(partPath); err == nil || !os.IsNotExist(err) {
		t.Fatalf("partial file should be removed, err=%v", err)
	}
}

func TestCacheKeyFallsBackToHash(t *testing.T) {
	t.Parallel()
	key := cacheKey("https://example.com/foo.pdf")
	if len(key) == 0 {
		t.Fatal("cache key empty")
	}
	if strings.Contains(key, "/") {
		t.Fatalf("cache key should be sanitized, got %q", key)
	}
}
