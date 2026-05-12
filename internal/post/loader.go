package post

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Index is the result of scanning a vault — every published post, keyed by
// slug, plus the slice form sorted newest-first for index and RSS use.
type Index struct {
	BySlug map[string]Post
	Sorted []Post // newest date first; ties broken by slug
}

// Load walks `vaultRoot/blog/` recursively and returns an Index of every
// published `index.md` it finds. Drafts and stray files are skipped; YAML
// parse failures and duplicate slugs are returned as errors so we don't
// silently publish a half-broken site.
//
// `defaultLoc` is the timezone used to interpret bare YYYY-MM-DD
// frontmatter dates (pass `time.UTC` for stable test output, or
// `time.Local` / a specific `time.LoadLocation` result in production).
func Load(vaultRoot string, defaultLoc *time.Location) (*Index, error) {
	blogRoot := filepath.Join(vaultRoot, "blog")
	info, err := os.Stat(blogRoot)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &Index{BySlug: map[string]Post{}}, nil
		}
		return nil, fmt.Errorf("stat %s: %w", blogRoot, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", blogRoot)
	}

	idx := &Index{BySlug: make(map[string]Post)}

	err = filepath.WalkDir(blogRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip Obsidian's dot-folders and our `_drafts/` convention.
			name := d.Name()
			if path != blogRoot && (strings.HasPrefix(name, ".") || name == "_drafts") {
				return fs.SkipDir
			}
			return nil
		}
		if d.Name() != "index.md" {
			return nil
		}

		dir := filepath.Dir(path)
		folderName := filepath.Base(dir)

		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}

		p, err := Parse(content, dir, path, folderName, defaultLoc)
		if err != nil {
			if errors.Is(err, ErrDraft) {
				return nil
			}
			return fmt.Errorf("%s: %w", path, err)
		}

		if existing, ok := idx.BySlug[p.Slug]; ok {
			return fmt.Errorf("duplicate slug %q: %s and %s", p.Slug, existing.Path, p.Path)
		}
		idx.BySlug[p.Slug] = p
		return nil
	})
	if err != nil {
		return nil, err
	}

	idx.Sorted = make([]Post, 0, len(idx.BySlug))
	for _, p := range idx.BySlug {
		idx.Sorted = append(idx.Sorted, p)
	}
	sort.Slice(idx.Sorted, func(i, j int) bool {
		if idx.Sorted[i].Date.Equal(idx.Sorted[j].Date) {
			return idx.Sorted[i].Slug < idx.Sorted[j].Slug
		}
		return idx.Sorted[i].Date.After(idx.Sorted[j].Date)
	})
	return idx, nil
}
