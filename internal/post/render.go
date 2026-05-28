package post

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// PerPostTemplateFile is the filename a post can drop next to its index.md
// to take over the whole page rendering. Its absence means the default
// template (templates/base.html) is used.
const PerPostTemplateFile = "template.html"

// Rendered is the output of Render: the post's HTML body (after markdown
// conversion + asset-path rewriting), a per-post template if one exists,
// and the post metadata that templates and the index/RSS handlers need.
type Rendered struct {
	Post         Post
	BodyHTML     template.HTML // markdown rendered to HTML
	Image        string // first image URL from the post body (rewritten path), empty if none
	PerPostTmpl  *template.Template // nil if no template.html in the post folder
}

// TemplateData is the value passed into either the default base template or
// a per-post template.html. Kept small on purpose — anything a post template
// wants beyond this should be reachable from .Post.
type TemplateData struct {
	Title   string
	Slug    string
	Date    time.Time
	Summary string
	Content template.HTML
	Image   string // absolute URL to the post's preview image, empty if none
}

// Renderer turns a Post into HTML, with two responsibilities the goldmark
// defaults don't handle: rewriting relative image/link URLs to point at the
// per-slug asset route (/posts/<slug>/...), and loading the optional
// per-post template.html.
type Renderer struct {
	md goldmark.Markdown
}

// NewRenderer builds a goldmark instance with the extensions we want:
// GFM-style tables/strikethrough, fenced code blocks (default), inline raw
// HTML (default — that's how the "tabs scaffold" use case works), and
// unsafe HTML output enabled because posts are trusted (authored by Tyler
// himself, not user-submitted).
func NewRenderer() *Renderer {
	md := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			html.WithUnsafe(),
		),
	)
	return &Renderer{md: md}
}

// Render produces the HTML body for a post and loads its per-post template
// if one exists on disk.
func (r *Renderer) Render(p Post) (*Rendered, error) {
	rewritten, imgURL, err := r.renderBody(p)
	if err != nil {
		return nil, fmt.Errorf("render markdown for %s: %w", p.Slug, err)
	}

	out := &Rendered{
		Post:     p,
		BodyHTML: template.HTML(rewritten),
		Image:    imgURL,
	}

	tmplPath := filepath.Join(p.Dir, PerPostTemplateFile)
	tmplBytes, err := os.ReadFile(tmplPath)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("read per-post template %s: %w", tmplPath, err)
		}
		return out, nil
	}
	t, err := template.New(p.Slug).Parse(string(tmplBytes))
	if err != nil {
		return nil, fmt.Errorf("parse per-post template %s: %w", tmplPath, err)
	}
	out.PerPostTmpl = t
	return out, nil
}

func (r *Renderer) renderBody(p Post) ([]byte, string, error) {
	source := []byte(p.Body)
	doc := r.md.Parser().Parse(text.NewReader(source))
	rewriteRelativeURLs(doc, source, p.Slug)

	imgURL := firstImageURL(doc)

	var buf bytes.Buffer
	if err := r.md.Renderer().Render(&buf, source, doc); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), imgURL, nil
}

// rewriteRelativeURLs walks the AST and rewrites any relative image src or
// link href to point at the per-slug asset route. Absolute URLs (anything
// with a scheme, or starting with `/`, `#`, or `mailto:`) are left alone.
func rewriteRelativeURLs(root ast.Node, source []byte, slug string) {
	_ = util.Prioritized // keep import; reserved for future extension hooks
	ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch node := n.(type) {
		case *ast.Image:
			node.Destination = []byte(rewriteURL(string(node.Destination), slug))
		case *ast.Link:
			node.Destination = []byte(rewriteURL(string(node.Destination), slug))
		}
		return ast.WalkContinue, nil
	})
}

func rewriteURL(raw, slug string) string {
	if raw == "" {
		return raw
	}
	if strings.HasPrefix(raw, "#") || strings.HasPrefix(raw, "/") {
		return raw
	}
	if u, err := url.Parse(raw); err == nil && u.IsAbs() {
		return raw
	}
	if strings.HasPrefix(raw, "mailto:") {
		return raw
	}
	// Use path.Join (not filepath.Join) so we get forward slashes on every OS.
	return path.Join("/posts", slug, raw)
}

func firstImageURL(root ast.Node) string {
	var dest string
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if img, ok := n.(*ast.Image); ok {
			dest = string(img.Destination)
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})
	return dest
}

// FirstParagraphText extracts the first paragraph of the rendered HTML as
// plain text, stripping all tags. Used by the RSS generator when a post
// doesn't supply an explicit `summary:` frontmatter field.
func FirstParagraphText(html template.HTML) string {
	s := string(html)
	start := strings.Index(s, "<p>")
	if start < 0 {
		return strings.TrimSpace(stripTags(s))
	}
	end := strings.Index(s[start:], "</p>")
	if end < 0 {
		return strings.TrimSpace(stripTags(s[start+3:]))
	}
	return strings.TrimSpace(stripTags(s[start+3 : start+end]))
}

func stripTags(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '<':
			in = true
		case r == '>':
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return b.String()
}
