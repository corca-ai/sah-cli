package sah

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/sandrolain/httpcache"
	"github.com/sandrolain/httpcache/diskcache"
)

const maxRedirects = 10

func newHTTPClient(transport http.RoundTripper) *http.Client {
	if transport == nil {
		transport = clonedDefaultTransport()
	}
	return &http.Client{
		Timeout:       45 * time.Second,
		Transport:     transport,
		CheckRedirect: followRedirects,
	}
}

func followRedirects(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("stopped after %d redirects", maxRedirects)
	}
	if len(via) == 0 {
		return nil
	}

	previous := via[len(via)-1]
	if shouldPreserveRedirectMethodAndBody(previous, req) {
		if err := restoreRedirectMethodAndBody(req, previous); err != nil {
			return err
		}
	}
	if shouldPreserveSensitiveHeadersOnRedirect(previous.URL, req.URL) {
		copyRequestHeaderIfPresent(req.Header, previous.Header, "Authorization")
		copyRequestHeaderIfPresent(req.Header, previous.Header, "X-API-Key")
	} else {
		req.Header.Del("Authorization")
		req.Header.Del("X-API-Key")
	}
	return nil
}

func shouldPreserveRedirectMethodAndBody(previous *http.Request, req *http.Request) bool {
	statusCode := redirectStatusCode(previous, req)
	switch statusCode {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther:
	default:
		return false
	}

	method := strings.ToUpper(strings.TrimSpace(previous.Method))
	return method != http.MethodGet && method != http.MethodHead
}

func redirectStatusCode(previous *http.Request, req *http.Request) int {
	if req != nil && req.Response != nil {
		return req.Response.StatusCode
	}
	if previous != nil && previous.Response != nil {
		return previous.Response.StatusCode
	}
	return 0
}

func restoreRedirectMethodAndBody(req *http.Request, previous *http.Request) error {
	req.Method = previous.Method
	req.GetBody = previous.GetBody
	req.ContentLength = previous.ContentLength
	if previous.GetBody != nil {
		body, err := previous.GetBody()
		if err != nil {
			return err
		}
		req.Body = body
	} else {
		req.Body = previous.Body
	}
	copyRequestHeaderIfPresent(req.Header, previous.Header, "Content-Type")
	return nil
}

func shouldPreserveSensitiveHeadersOnRedirect(fromURL *url.URL, toURL *url.URL) bool {
	if !isHTTPRedirectURL(fromURL) || !isHTTPRedirectURL(toURL) {
		return false
	}
	if strings.EqualFold(fromURL.Scheme, "https") && !strings.EqualFold(toURL.Scheme, "https") {
		return false
	}
	return canonicalRedirectHost(fromURL) == canonicalRedirectHost(toURL)
}

func isHTTPRedirectURL(raw *url.URL) bool {
	if raw == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(raw.Scheme)) {
	case "http", "https":
		return true
	default:
		return false
	}
}

func canonicalRedirectHost(raw *url.URL) string {
	if raw == nil {
		return ""
	}
	host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw.Hostname()), "."))
	host = strings.TrimPrefix(host, "www.")
	if host == "" {
		return ""
	}
	port := raw.Port()
	switch {
	case port == "":
		return host
	case strings.EqualFold(raw.Scheme, "http") && port == "80":
		return host
	case strings.EqualFold(raw.Scheme, "https") && port == "443":
		return host
	default:
		return host + ":" + port
	}
}

func copyRequestHeaderIfPresent(target http.Header, source http.Header, key string) {
	if target == nil || source == nil {
		return
	}
	values, ok := source[http.CanonicalHeaderKey(key)]
	if !ok {
		return
	}
	target[http.CanonicalHeaderKey(key)] = append([]string(nil), values...)
}

func clonedDefaultTransport() http.RoundTripper {
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return http.DefaultTransport
	}
	return defaultTransport.Clone()
}

func buildCachedTransport(paths Paths) http.RoundTripper {
	cacheDir := strings.TrimSpace(paths.HTTPCacheDir)
	if cacheDir == "" {
		return clonedDefaultTransport()
	}
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return clonedDefaultTransport()
	}

	transport := httpcache.NewTransport(diskcache.New(cacheDir))
	transport.Transport = clonedDefaultTransport()
	transport.CacheKeyHeaders = []string{
		"X-API-Key",
		"Authorization",
		"Accept",
		"X-SAH-CLI-Version",
	}
	transport.SkipServerErrorsFromCache = true
	transport.DisableWarningHeader = true
	return transport
}
