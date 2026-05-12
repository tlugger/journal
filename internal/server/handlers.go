package server

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/tlugger/journal/internal/feed"
	"github.com/tlugger/journal/internal/post"
)

// indexPageData is the value passed to templates/index.html.
type indexPageData struct {
	Posts []post.Post
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	idx := s.Index()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.indexTmpl.Execute(w, indexPageData{Posts: idx.Sorted}); err != nil {
		http.Error(w, "render index", http.StatusInternalServerError)
	}
}

func (s *Server) handlePost(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	idx := s.Index()
	p, ok := idx.BySlug[slug]
	if !ok {
		http.NotFound(w, r)
		return
	}

	rendered, err := s.renderedFor(p)
	if err != nil {
		http.Error(w, "render post", http.StatusInternalServerError)
		return
	}

	data := post.TemplateData{
		Title:   p.Title,
		Slug:    p.Slug,
		Date:    p.Date,
		Summary: p.Summary,
		Content: rendered.BodyHTML,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl := s.postTmpl
	if rendered.PerPostTmpl != nil {
		tmpl = rendered.PerPostTmpl
	}
	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, "render post template", http.StatusInternalServerError)
	}
}

// renderedFor returns the cached render for p, populating the cache on
// first access. Concurrent callers may race to render the same post — that
// is a bounded waste of CPU on a Pi, not a correctness issue.
func (s *Server) renderedFor(p post.Post) (*post.Rendered, error) {
	if r := s.cache.Get(p.Slug); r != nil {
		return r, nil
	}
	r, err := s.renderer.Render(p)
	if err != nil {
		return nil, err
	}
	s.cache.Put(r)
	return r, nil
}

// handleAsset serves any file (image, css, js) co-located with a post's
// index.md, under /posts/<slug>/<path>. Path traversal is blocked by
// rejecting any cleaned asset path that escapes the post directory.
func (s *Server) handleAsset(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	asset := r.PathValue("asset")
	if asset == "" || strings.Contains(asset, "..") {
		http.NotFound(w, r)
		return
	}

	idx := s.Index()
	p, ok := idx.BySlug[slug]
	if !ok {
		http.NotFound(w, r)
		return
	}

	// Disallow serving the markdown source itself or the per-post template
	// — those are implementation details, not assets.
	switch filepath.Base(asset) {
	case "index.md", post.PerPostTemplateFile:
		http.NotFound(w, r)
		return
	}

	full := filepath.Join(p.Dir, asset)
	cleaned := filepath.Clean(full)
	postDirClean := filepath.Clean(p.Dir) + string(filepath.Separator)
	if !strings.HasPrefix(cleaned+string(filepath.Separator), postDirClean) {
		http.NotFound(w, r)
		return
	}

	f, err := os.Open(cleaned)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "open asset", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		http.Error(w, "stat asset", http.StatusInternalServerError)
		return
	}
	if stat.IsDir() {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, filepath.Base(cleaned), stat.ModTime(), f)
}

func (s *Server) handleRSS(w http.ResponseWriter, r *http.Request) {
	idx := s.Index()
	items := make([]feed.Item, 0, len(idx.Sorted))
	for _, p := range idx.Sorted {
		summary := p.Summary
		if summary == "" {
			rendered, err := s.renderedFor(p)
			if err == nil {
				summary = post.FirstParagraphText(rendered.BodyHTML)
			}
		}
		items = append(items, feed.Item{
			Title:       p.Title,
			Slug:        p.Slug,
			Date:        p.Date,
			Description: summary,
		})
	}

	cfg := feed.Config{
		Title:       s.cfg.SiteTitle,
		Link:        s.cfg.SiteURL,
		Description: s.cfg.SiteDesc,
		SelfURL:     strings.TrimRight(s.cfg.SiteURL, "/") + "/rss.xml",
		Author:      s.cfg.FeedAuthor,
	}
	body, err := feed.Build(cfg, items, s.cfg.Now())
	if err != nil {
		http.Error(w, fmt.Sprintf("build rss: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	_, _ = w.Write(body)
}
