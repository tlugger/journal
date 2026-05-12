package server

import (
	"sync"

	"github.com/tlugger/journal/internal/post"
)

// Cache memoizes rendered posts keyed by slug. The slug→source-file map is
// owned by post.Index; this cache only stores the rendered output so a
// vault-sync change can invalidate without re-walking the filesystem.
//
// All public methods are safe for concurrent use by the HTTP handlers.
type Cache struct {
	mu       sync.RWMutex
	rendered map[string]*post.Rendered
}

func NewCache() *Cache {
	return &Cache{rendered: make(map[string]*post.Rendered)}
}

// Get returns the cached rendered post for a slug, or nil if not present.
func (c *Cache) Get(slug string) *post.Rendered {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.rendered[slug]
}

// Put stores a rendered post under its slug.
func (c *Cache) Put(r *post.Rendered) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rendered[r.Post.Slug] = r
}

// Invalidate removes a single slug from the cache, e.g. after fsnotify
// observes an edit to one post.
func (c *Cache) Invalidate(slug string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.rendered, slug)
}

// Clear drops every cached entry. Called by the fsnotify watcher on any
// change to the vault — cheap because rendering is on-demand on the next
// request and the working set is small.
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rendered = make(map[string]*post.Rendered)
}

// Size is exposed for tests and /healthz-style introspection.
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.rendered)
}
