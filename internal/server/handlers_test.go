package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tlugger/journal/internal/assets"
)

// repoRoot is the absolute path to the journal repo root, derived once.
// Used only to locate testdata/vault — templates and static now come from
// the embedded assets package, not from disk.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// internal/server/ → ../../
	root, err := filepath.Abs(filepath.Join(wd, "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	root := repoRoot(t)
	s, err := New(Config{
		VaultDir:  filepath.Join(root, "testdata", "vault"),
		Templates: assets.Templates,
		Static:    assets.Static,
		SiteURL:   "https://blog.test.local",
		SiteTitle: "test blog",
		SiteDesc:  "test",
		Now:       func() time.Time { return time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.NewMux())
	t.Cleanup(ts.Close)
	return ts
}

func getString(t *testing.T, ts *httptest.Server, path string) (int, string, http.Header) {
	t.Helper()
	resp, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(body), resp.Header
}

func TestHandleIndex_ListsPublishedNewestFirst(t *testing.T) {
	ts := newTestServer(t)
	status, body, _ := getString(t, ts, "/")
	if status != 200 {
		t.Fatalf("status = %d", status)
	}
	// custom-template (2026-05-11) should appear before first-post (2026-05-10).
	custIdx := strings.Index(body, "/posts/custom-template")
	firstIdx := strings.Index(body, "/posts/first-post")
	if custIdx < 0 || firstIdx < 0 {
		t.Fatalf("missing published posts in index: %s", body)
	}
	if custIdx > firstIdx {
		t.Errorf("custom-template should appear before first-post in newest-first order")
	}
	if strings.Contains(body, "/posts/draft-post") {
		t.Errorf("draft-post should not appear on index")
	}
	if !strings.Contains(body, "/posts/no-slug-folder") {
		t.Errorf("no-slug-folder should appear (folder-name slug fallback)")
	}
}

func TestHandlePost_PublishedSlug(t *testing.T) {
	ts := newTestServer(t)
	status, body, hdr := getString(t, ts, "/posts/first-post")
	if status != 200 {
		t.Fatalf("status = %d", status)
	}
	if !strings.HasPrefix(hdr.Get("Content-Type"), "text/html") {
		t.Errorf("content-type = %q", hdr.Get("Content-Type"))
	}
	if !strings.Contains(body, "First post") {
		t.Errorf("missing title in body: %s", body)
	}
	if !strings.Contains(body, `src="/posts/first-post/image.png"`) {
		t.Errorf("image not rewritten: %s", body)
	}
}

func TestHandlePost_MissingSlug404(t *testing.T) {
	ts := newTestServer(t)
	status, _, _ := getString(t, ts, "/posts/nope")
	if status != 404 {
		t.Errorf("status = %d, want 404", status)
	}
}

func TestHandlePost_DraftIsNotReachable(t *testing.T) {
	ts := newTestServer(t)
	status, _, _ := getString(t, ts, "/posts/draft-post")
	if status != 404 {
		t.Errorf("status = %d, want 404", status)
	}
}

func TestHandlePost_CustomTemplateUsed(t *testing.T) {
	ts := newTestServer(t)
	status, body, _ := getString(t, ts, "/posts/custom-template")
	if status != 200 {
		t.Fatalf("status = %d", status)
	}
	if !strings.Contains(body, "CUSTOM: Custom template post") {
		t.Errorf("custom template not used: %s", body)
	}
	if !strings.Contains(body, `data-slug="custom-template"`) {
		t.Errorf("custom template data-slug not substituted: %s", body)
	}
}

func TestHandleAsset_ServesImage(t *testing.T) {
	ts := newTestServer(t)
	status, body, hdr := getString(t, ts, "/posts/first-post/image.png")
	if status != 200 {
		t.Fatalf("status = %d", status)
	}
	if !strings.HasPrefix(hdr.Get("Content-Type"), "image/png") {
		t.Errorf("content-type = %q", hdr.Get("Content-Type"))
	}
	if len(body) == 0 || !strings.HasPrefix(body, "\x89PNG") {
		end := len(body)
		if end > 8 {
			end = 8
		}
		t.Errorf("body not a PNG: % x", body[:end])
	}
}

func TestHandleAsset_PathTraversalBlocked(t *testing.T) {
	ts := newTestServer(t)
	cases := []string{
		"/posts/first-post/../../../etc/passwd",
		"/posts/first-post/..%2F..%2Fetc%2Fpasswd",
		"/posts/first-post/%2e%2e/image.png",
	}
	for _, p := range cases {
		t.Run(p, func(t *testing.T) {
			status, _, _ := getString(t, ts, p)
			if status == 200 {
				t.Errorf("path traversal succeeded for %s (status %d)", p, status)
			}
		})
	}
}

func TestHandleAsset_MarkdownAndTemplateHidden(t *testing.T) {
	ts := newTestServer(t)
	for _, p := range []string{"/posts/first-post/index.md", "/posts/custom-template/template.html"} {
		t.Run(p, func(t *testing.T) {
			status, _, _ := getString(t, ts, p)
			if status != 404 {
				t.Errorf("expected 404 for %s, got %d", p, status)
			}
		})
	}
}

func TestHandleAsset_MissingSlug404(t *testing.T) {
	ts := newTestServer(t)
	status, _, _ := getString(t, ts, "/posts/no-such-slug/foo.png")
	if status != 404 {
		t.Errorf("status = %d", status)
	}
}

func TestHandleRSS_ContentTypeAndShape(t *testing.T) {
	ts := newTestServer(t)
	status, body, hdr := getString(t, ts, "/rss.xml")
	if status != 200 {
		t.Fatalf("status = %d", status)
	}
	if !strings.HasPrefix(hdr.Get("Content-Type"), "application/rss+xml") {
		t.Errorf("content-type = %q", hdr.Get("Content-Type"))
	}
	if !strings.Contains(body, "<rss version=\"2.0\"") {
		t.Errorf("not an RSS doc: %s", body)
	}
	if !strings.Contains(body, "https://blog.test.local/posts/first-post") {
		t.Errorf("missing first-post link in feed")
	}
	if strings.Contains(body, "draft-post") {
		t.Errorf("draft leaked into RSS")
	}
}

func TestHandleFaviconShortcut(t *testing.T) {
	ts := newTestServer(t)
	status, body, hdr := getString(t, ts, "/favicon.ico")
	if status != 200 {
		t.Fatalf("status = %d", status)
	}
	if !strings.HasPrefix(hdr.Get("Content-Type"), "image/") {
		t.Errorf("content-type = %q", hdr.Get("Content-Type"))
	}
	if len(body) == 0 {
		t.Errorf("favicon body empty")
	}
}

func TestHandleHealthz(t *testing.T) {
	ts := newTestServer(t)
	status, body, _ := getString(t, ts, "/healthz")
	if status != 200 || strings.TrimSpace(body) != "ok" {
		t.Errorf("healthz returned status=%d body=%q", status, body)
	}
}

func TestReloadIndex_PicksUpNewPost(t *testing.T) {
	// Create a temp vault with one post; serve; mutate; reload.
	dir := t.TempDir()
	mustWritePost(t, filepath.Join(dir, "blog", "one"), "one", "2026-05-01")

	s, err := New(Config{
		VaultDir:  dir,
		Templates: assets.Templates,
		Static:    assets.Static,
		SiteURL:   "https://blog.test.local",
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.NewMux())
	defer ts.Close()

	status, _, _ := getString(t, ts, "/posts/two")
	if status != 404 {
		t.Fatalf("status = %d, expected 404 before reload", status)
	}

	mustWritePost(t, filepath.Join(dir, "blog", "two"), "two", "2026-05-02")
	if err := s.ReloadIndex(); err != nil {
		t.Fatal(err)
	}
	status, _, _ = getString(t, ts, "/posts/two")
	if status != 200 {
		t.Errorf("status = %d after reload, expected 200", status)
	}
}

func mustWritePost(t *testing.T, dir, slug, date string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\ntitle: T\nslug: " + slug + "\ndate: " + date + "\npublished: true\n---\n\nbody for " + slug + "\n"
	if err := os.WriteFile(filepath.Join(dir, "index.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

