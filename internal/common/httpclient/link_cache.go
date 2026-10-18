package httpclient

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/feruzabad/debroxy/internal/common/clock"
)

// LinkCache keeps resolved secret links (CDN URLs) in memory for at most
// maxAge (ADR 0010 rule 8). It is never persisted, dumped or logged.
type LinkCache struct {
	mu      sync.Mutex
	maxAge  time.Duration
	clock   clock.Clock
	entries map[string]cachedLink
}

type cachedLink struct {
	url      SecretURL
	storedAt time.Time
}

func NewLinkCache(maxAge time.Duration, clk clock.Clock) *LinkCache {
	return &LinkCache{maxAge: maxAge, clock: clk, entries: map[string]cachedLink{}}
}

func (c *LinkCache) get(key string) (SecretURL, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return SecretURL{}, false
	}
	if c.clock.Now().Sub(entry.storedAt) >= c.maxAge {
		delete(c.entries, key)
		return SecretURL{}, false
	}
	return entry.url, true
}

func (c *LinkCache) put(key string, url SecretURL) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.clock.Now()
	for k, entry := range c.entries { // drop expired entries as we go
		if now.Sub(entry.storedAt) >= c.maxAge {
			delete(c.entries, k)
		}
	}
	c.entries[key] = cachedLink{url: url, storedAt: now}
}

// Drop forgets one entry.
func (c *LinkCache) Drop(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}

// DropPrefix forgets every entry whose key starts with prefix, e.g. all
// targets of a deleted item.
func (c *LinkCache) DropPrefix(prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key := range c.entries {
		if strings.HasPrefix(key, prefix) {
			delete(c.entries, key)
		}
	}
}

// LinkRequest is a request to a secret link that can be resolved again.
type LinkRequest struct {
	CacheKey string
	// Resolve asks the provider API for a fresh link.
	Resolve func(ctx context.Context) (SecretURL, error)
	Method  string
	Header  http.Header
	// Bucket is nil for requests without a rate class (CDN file downloads).
	Bucket *BucketKey
	// MaxWait bounds waiting for Bucket.
	MaxWait time.Duration
}

// staleLinkStatus: an upstream answer meaning the link is no longer valid.
func staleLinkStatus(status int) bool {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusGone:
		return true
	}
	return false
}

// OpenLink sends a request to a cached or freshly resolved link. On
// 401/403/404/410 before any byte is read, it drops the cached link,
// resolves once more and retries once (ADR 0010 rule 3). The body is not
// bounded in time; ctx ends it. The caller closes the response body.
func (c *Client) OpenLink(ctx context.Context, cache *LinkCache, request LinkRequest) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		link, cached := cache.get(request.CacheKey)
		if !cached {
			resolved, err := request.Resolve(ctx)
			if err != nil {
				return nil, err
			}
			link = resolved
			cache.put(request.CacheKey, link)
		}
		response, err := c.Do(ctx, Request{
			Method: request.Method, URL: link, Header: request.Header,
			Bucket: request.Bucket, MaxWait: request.MaxWait, Idempotent: true,
		})
		if err != nil {
			return nil, err
		}
		if attempt == 0 && staleLinkStatus(response.StatusCode) {
			drainAndClose(response)
			cache.Drop(request.CacheKey)
			continue
		}
		return response, nil
	}
}
