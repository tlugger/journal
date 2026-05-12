// Package post models a single blog post backed by a markdown file in an
// Obsidian vault. It is the source of truth for what "a post" means: the
// frontmatter fields we accept, how drafts are recognized, and what counts
// as a parse failure versus a soft skip.
package post

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v2"
)

// Post represents a single published blog post loaded from disk.
//
// The Path/Dir fields are absolute filesystem paths so callers (renderer,
// asset handler, fsnotify) can locate the source and its co-located assets
// without re-walking the vault.
type Post struct {
	Title   string
	Slug    string
	Date    time.Time
	Summary string
	Body    string // raw markdown body, after frontmatter is stripped
	Path    string // absolute path to the index.md file
	Dir     string // absolute path to the post's directory
}

type rawFrontmatter struct {
	Title     string `yaml:"title"`
	Slug      string `yaml:"slug"`
	Date      string `yaml:"date"`
	Summary   string `yaml:"summary"`
	Published *bool  `yaml:"published"`
}

// ErrDraft is returned by Parse when the file's frontmatter does not opt into
// publication (missing `published` or `published: false`). It's an error
// rather than a (nil, nil) return so the loader can decide to skip without
// silently masking real parse failures.
var ErrDraft = errors.New("post is a draft (published != true)")

// ErrNoFrontmatter is returned when the file does not start with a
// `---` YAML frontmatter block.
var ErrNoFrontmatter = errors.New("post is missing frontmatter")

// Parse reads a markdown file's bytes and produces a Post.
//
// `dir` is the directory holding the file (used for the folder-name slug
// fallback and stored on the result). `path` is the absolute path to the
// file itself. The returned ErrDraft means "skip this", everything else
// means "the file is broken and the user should know".
func Parse(content []byte, dir, path, folderName string) (Post, error) {
	body, fm, err := splitFrontmatter(content)
	if err != nil {
		return Post{}, err
	}

	var raw rawFrontmatter
	if err := yaml.Unmarshal(fm, &raw); err != nil {
		return Post{}, fmt.Errorf("parse frontmatter: %w", err)
	}

	if raw.Published == nil || !*raw.Published {
		return Post{}, ErrDraft
	}

	if raw.Date == "" {
		return Post{}, errors.New("frontmatter: missing required field `date`")
	}
	date, err := parseDate(raw.Date)
	if err != nil {
		return Post{}, fmt.Errorf("frontmatter: invalid date %q: %w", raw.Date, err)
	}

	slug := strings.TrimSpace(raw.Slug)
	if slug == "" {
		slug = folderName
	}
	if slug == "" {
		return Post{}, errors.New("frontmatter: missing `slug` and no folder name to fall back on")
	}

	return Post{
		Title:   strings.TrimSpace(raw.Title),
		Slug:    slug,
		Date:    date,
		Summary: strings.TrimSpace(raw.Summary),
		Body:    string(body),
		Path:    path,
		Dir:     dir,
	}, nil
}

// splitFrontmatter peels a leading `---\n...\n---\n` YAML block off the
// content and returns (body, frontmatter, err). If there is no frontmatter
// at all, ErrNoFrontmatter is returned — the loader treats this as a hard
// failure so a stray .md file in `vault/blog/` doesn't silently become a
// post with empty metadata.
func splitFrontmatter(content []byte) (body, fm []byte, err error) {
	// Normalize CRLF early so the index calculations work regardless of
	// where the file was authored.
	content = bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))

	if !bytes.HasPrefix(content, []byte("---\n")) {
		return nil, nil, ErrNoFrontmatter
	}

	rest := content[4:]
	end := bytes.Index(rest, []byte("\n---\n"))
	if end == -1 {
		// Allow trailing `---` with no newline after, for the case where
		// a file is exactly frontmatter + nothing.
		if bytes.HasSuffix(rest, []byte("\n---")) {
			return nil, rest[:len(rest)-4], nil
		}
		return nil, nil, fmt.Errorf("frontmatter: closing `---` not found")
	}

	return rest[end+5:], rest[:end], nil
}

// parseDate accepts a few common shapes from frontmatter: the YYYY-MM-DD
// shorthand (which is what the Obsidian template emits), and full RFC3339
// timestamps if you want a specific publish time of day.
func parseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("expected YYYY-MM-DD or RFC3339")
}
