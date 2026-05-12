package server

import (
	"sync"
	"testing"

	"github.com/tlugger/journal/internal/post"
)

func TestCache_PutGet(t *testing.T) {
	c := NewCache()
	r := &post.Rendered{Post: post.Post{Slug: "a"}}
	c.Put(r)
	if got := c.Get("a"); got != r {
		t.Errorf("Get(a) = %v, want %v", got, r)
	}
	if got := c.Get("missing"); got != nil {
		t.Errorf("Get(missing) = %v, want nil", got)
	}
	if c.Size() != 1 {
		t.Errorf("Size = %d, want 1", c.Size())
	}
}

func TestCache_Invalidate(t *testing.T) {
	c := NewCache()
	c.Put(&post.Rendered{Post: post.Post{Slug: "a"}})
	c.Put(&post.Rendered{Post: post.Post{Slug: "b"}})
	c.Invalidate("a")
	if c.Get("a") != nil {
		t.Error("a should be invalidated")
	}
	if c.Get("b") == nil {
		t.Error("b should still be present")
	}
}

func TestCache_Clear(t *testing.T) {
	c := NewCache()
	c.Put(&post.Rendered{Post: post.Post{Slug: "a"}})
	c.Put(&post.Rendered{Post: post.Post{Slug: "b"}})
	c.Clear()
	if c.Size() != 0 {
		t.Errorf("after Clear, Size = %d", c.Size())
	}
}

// TestCache_ConcurrentAccess is the reason the cache uses sync.RWMutex; run
// this with `-race` to exercise it.
func TestCache_ConcurrentAccess(t *testing.T) {
	c := NewCache()
	var wg sync.WaitGroup
	const writers, readers, ops = 4, 8, 200
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < ops; i++ {
				slug := "w" + string(rune('a'+id))
				c.Put(&post.Rendered{Post: post.Post{Slug: slug}})
				if i%10 == 0 {
					c.Invalidate(slug)
				}
			}
		}(w)
	}
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < ops; i++ {
				_ = c.Get("wa")
				_ = c.Size()
			}
		}()
	}
	wg.Wait()
}
