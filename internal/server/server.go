// Package server wires the HTTP routes for blog.tylerkno.ws. The Server
// struct owns the post Index, the render cache, and the parsed default
// templates; routes hang off NewMux().
package server

import (
	"fmt"
	"html/template"
	"io/fs"
	"mime"
	"net/http"
	"sync"
	"time"

	"github.com/tlugger/journal/internal/post"
)

func init() {
	// Go's default MIME map doesn't include the `.webmanifest` extension;
	// Chrome strict-checks the manifest MIME for PWA features.
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

// Config controls runtime behavior: where the vault lives, which fs.FS
// supplies templates and static assets, and the canonical site URL used
// in RSS.
//
// Templates and Static are filesystem-shaped because the caller chooses
// between the embedded asset bundle (`internal/assets`) and an on-disk
// `os.DirFS` override for local dev iteration without rebuilds.
type Config struct {
	VaultDir   string
	Templates  fs.FS // expects index.html, base.html at the root
	Static     fs.FS // served at /static/; favicon.ico expected under favicon/
	SiteURL    string
	SiteTitle  string
	SiteDesc   string
	FeedAuthor string
	Now        func() time.Time // injectable for tests
}

// Server is the long-lived HTTP application state. Public methods are safe
// for concurrent use; ReloadIndex is what the fsnotify watcher in main.go
// calls after a vault change.
type Server struct {
	cfg      Config
	renderer *post.Renderer
	cache    *Cache

	mu        sync.RWMutex
	index     *post.Index
	indexTmpl *template.Template
	postTmpl  *template.Template
}

// New builds a Server, performs the initial vault walk, and parses the
// default templates. A parse error here is fatal — the server can't serve.
func New(cfg Config) (*Server, error) {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.SiteTitle == "" {
		cfg.SiteTitle = "Tyler's blog"
	}
	if cfg.SiteDesc == "" {
		cfg.SiteDesc = "Posts by Tyler Lugger."
	}
	if cfg.Templates == nil {
		return nil, fmt.Errorf("server config: Templates fs.FS is required")
	}
	if cfg.Static == nil {
		return nil, fmt.Errorf("server config: Static fs.FS is required")
	}

	s := &Server{
		cfg:      cfg,
		renderer: post.NewRenderer(),
		cache:    NewCache(),
	}

	indexTmpl, err := template.ParseFS(cfg.Templates, "index.html")
	if err != nil {
		return nil, fmt.Errorf("parse index template: %w", err)
	}
	postTmpl, err := template.ParseFS(cfg.Templates, "base.html")
	if err != nil {
		return nil, fmt.Errorf("parse base template: %w", err)
	}
	s.indexTmpl = indexTmpl
	s.postTmpl = postTmpl

	if err := s.ReloadIndex(); err != nil {
		return nil, fmt.Errorf("initial vault load: %w", err)
	}
	return s, nil
}

// ReloadIndex re-walks the vault and clears the render cache. The fsnotify
// watcher in cmd/blog/main.go calls this on debounced changes; tests call
// it directly. Errors are returned (and logged by the caller) but do not
// drop the previous good state.
func (s *Server) ReloadIndex() error {
	idx, err := post.Load(s.cfg.VaultDir)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.index = idx
	s.mu.Unlock()
	s.cache.Clear()
	return nil
}

// Index returns the current index. Always non-nil after New succeeds.
func (s *Server) Index() *post.Index {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.index
}

// NewMux returns an http.Handler with every route registered. Caller owns
// the http.Server (graceful shutdown, addr, timeouts).
func (s *Server) NewMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /posts/{slug}", s.handlePost)
	mux.HandleFunc("GET /posts/{slug}/{asset...}", s.handleAsset)
	mux.HandleFunc("GET /rss.xml", s.handleRSS)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	// Browsers probe /favicon.ico unconditionally; serve it from the
	// favicon bundle so dev-tools doesn't show a noisy 404.
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, s.cfg.Static, "favicon/favicon.ico")
	})
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(s.cfg.Static)))
	return mux
}
