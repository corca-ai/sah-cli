package sah

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"

	"github.com/peterbourgon/diskv"
	"github.com/sandrolain/httpcache"
)

const httpCacheSizeMax = 100 * 1024 * 1024

// httpcache's disk backend logs a warning when Delete misses on disk, but
// cache invalidation regularly deletes keys that were never materialized.
type quietDiskCache struct {
	disk *diskv.Diskv
}

func newQuietDiskCache(basePath string) httpcache.Cache {
	return &quietDiskCache{
		disk: diskv.New(diskv.Options{
			BasePath:     basePath,
			CacheSizeMax: httpCacheSizeMax,
		}),
	}
}

func (c *quietDiskCache) Get(key string) ([]byte, bool) {
	responseBytes, err := c.disk.Read(httpCacheFilename(key))
	if err != nil {
		return []byte{}, false
	}
	return responseBytes, true
}

func (c *quietDiskCache) Set(key string, responseBytes []byte) {
	filename := httpCacheFilename(key)
	if err := c.disk.WriteStream(filename, bytes.NewReader(responseBytes), true); err != nil {
		httpcache.GetLogger().Warn("failed to write to disk cache", "key", filename, "error", err)
	}
}

func (c *quietDiskCache) Delete(key string) {
	filename := httpCacheFilename(key)
	if err := c.disk.Erase(filename); err != nil && !errors.Is(err, os.ErrNotExist) {
		httpcache.GetLogger().Warn("failed to delete from disk cache", "key", filename, "error", err)
	}
}

func httpCacheFilename(key string) string {
	hash := sha256.New()
	_, _ = io.WriteString(hash, key)
	return hex.EncodeToString(hash.Sum(nil))
}
