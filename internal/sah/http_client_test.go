package sah

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sandrolain/httpcache"
)

func TestCachedTransportIgnoresMissingDiskDeletes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ok"))
	}))
	defer server.Close()

	var logs bytes.Buffer
	previousLogger := httpcache.GetLogger()
	httpcache.SetLogger(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer httpcache.SetLogger(previousLogger)

	paths := Paths{HTTPCacheDir: filepath.Join(t.TempDir(), "http-cache")}
	client := newHTTPClient(buildCachedTransport(paths))

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		t.Fatalf("drain response body: %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close response body: %v", err)
	}

	if strings.Contains(logs.String(), "failed to delete from disk cache") {
		t.Fatalf("unexpected disk cache delete warning: %s", logs.String())
	}
}
