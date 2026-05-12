// Command blog serves blog.tylerkno.ws — markdown posts walked out of a
// local copy of an Obsidian vault, rendered on demand, cached in memory,
// and refreshed when fsnotify spots a change.
//
// Flags / env: this binary is configured by env (so systemd can use a
// single EnvironmentFile) but also accepts CLI flags for local dev. Flags
// take precedence.
package main

import (
	"context"
	"errors"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/tlugger/journal/internal/assets"
	"github.com/tlugger/journal/internal/server"
)

// version is set at build time via -ldflags.
var version = "dev"

func main() {
	cfg, addr := parseFlags()

	log.Printf("blog %s starting; vault=%s addr=%s", version, cfg.VaultDir, addr)

	srv, err := server.New(cfg)
	if err != nil {
		log.Fatalf("init server: %v", err)
	}
	log.Printf("loaded %d posts", len(srv.Index().BySlug))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go watchVault(ctx, srv, cfg.VaultDir)

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.NewMux(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server: %v", err)
		}
	}()
	log.Printf("listening on %s", addr)

	<-ctx.Done()
	log.Printf("shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

func parseFlags() (server.Config, string) {
	addr := flag.String("addr", envOr("BLOG_ADDR", ":8106"), "HTTP listen address")
	vault := flag.String("vault", envOr("BLOG_VAULT_DIR", ""), "Path to the local Obsidian vault directory (must contain blog/)")
	tmplDir := flag.String("templates", envOr("BLOG_TEMPLATE_DIR", ""), "Optional override: read templates from this directory instead of the embedded bundle (for live dev iteration)")
	staticDir := flag.String("static", envOr("BLOG_STATIC_DIR", ""), "Optional override: read static assets from this directory instead of the embedded bundle")
	siteURL := flag.String("site-url", envOr("BLOG_SITE_URL", "https://blog.tylerkno.ws"), "Canonical site URL (used in RSS)")
	siteTitle := flag.String("site-title", envOr("BLOG_SITE_TITLE", "Tyler's blog"), "Site title")
	siteDesc := flag.String("site-desc", envOr("BLOG_SITE_DESC", "Engineering, self-hosting, baseball."), "Site description")
	feedAuthor := flag.String("feed-author", envOr("BLOG_FEED_AUTHOR", ""), "RSS author tag (e.g. \"you@example.com (Tyler)\")")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *showVersion {
		log.Printf("blog %s", version)
		os.Exit(0)
	}

	if *vault == "" {
		log.Fatal("must set -vault or BLOG_VAULT_DIR")
	}

	// Default to embedded assets; override with an on-disk directory only
	// if the user explicitly asks for it (handy for editing templates/CSS
	// without rebuilding the binary).
	templatesFS := assets.Templates
	if *tmplDir != "" {
		templatesFS = os.DirFS(*tmplDir)
		log.Printf("template override: reading from %s", *tmplDir)
	}
	staticFS := assets.Static
	if *staticDir != "" {
		staticFS = os.DirFS(*staticDir)
		log.Printf("static override: reading from %s", *staticDir)
	}

	return server.Config{
		VaultDir:   *vault,
		Templates:  templatesFS,
		Static:     staticFS,
		SiteURL:    *siteURL,
		SiteTitle:  *siteTitle,
		SiteDesc:   *siteDesc,
		FeedAuthor: *feedAuthor,
	}, *addr
}

func envOr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

// watchVault sets up an fsnotify watcher on every subdirectory of
// `vaultRoot/blog/` and triggers a debounced ReloadIndex when anything
// changes. fsnotify does not recurse on Linux, so we watch each dir
// individually and add new ones as they appear.
//
// Returns when ctx is cancelled.
func watchVault(ctx context.Context, srv *server.Server, vaultRoot string) {
	blogRoot := filepath.Join(vaultRoot, "blog")

	w, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("fsnotify: %v (live-reload disabled)", err)
		return
	}
	defer w.Close()

	addAll(w, blogRoot)

	const debounce = 500 * time.Millisecond
	var (
		timer   *time.Timer
		timerMu sync.Mutex
	)

	trigger := func() {
		timerMu.Lock()
		defer timerMu.Unlock()
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(debounce, func() {
			if err := srv.ReloadIndex(); err != nil {
				log.Printf("reload index: %v", err)
				return
			}
			log.Printf("reloaded index (%d posts)", len(srv.Index().BySlug))
		})
	}

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			// If a directory was created, watch it too so we pick up
			// edits inside it.
			if ev.Op&fsnotify.Create != 0 {
				if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
					if err := w.Add(ev.Name); err != nil {
						log.Printf("watch %s: %v", ev.Name, err)
					}
				}
			}
			trigger()
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			log.Printf("fsnotify error: %v", err)
		}
	}
}

func addAll(w *fsnotify.Watcher, root string) {
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			if err := w.Add(p); err != nil {
				log.Printf("watch %s: %v", p, err)
			}
		}
		return nil
	})
	if err != nil {
		log.Printf("walk for watcher: %v", err)
	}
}
