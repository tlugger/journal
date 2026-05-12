package post

import (
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRender_BasicMarkdown(t *testing.T) {
	r := NewRenderer()
	p := Post{Slug: "x", Body: "# Heading\n\nSome **bold** text.\n"}
	got, err := r.Render(p)
	if err != nil {
		t.Fatal(err)
	}
	body := string(got.BodyHTML)
	if !strings.Contains(body, "<h1") || !strings.Contains(body, ">Heading</h1>") {
		t.Errorf("expected h1 in output, got: %q", body)
	}
	if !strings.Contains(body, "<strong>bold</strong>") {
		t.Errorf("expected <strong>, got: %q", body)
	}
}

func TestRender_RelativeImageRewritten(t *testing.T) {
	r := NewRenderer()
	p := Post{Slug: "my-post", Body: "![diagram](image.png)\n"}
	got, err := r.Render(p)
	if err != nil {
		t.Fatal(err)
	}
	body := string(got.BodyHTML)
	if !strings.Contains(body, `src="/posts/my-post/image.png"`) {
		t.Errorf("image src not rewritten: %q", body)
	}
}

func TestRender_RelativeLinkRewritten(t *testing.T) {
	r := NewRenderer()
	p := Post{Slug: "a", Body: "[next](other.html)\n"}
	got, err := r.Render(p)
	if err != nil {
		t.Fatal(err)
	}
	body := string(got.BodyHTML)
	if !strings.Contains(body, `href="/posts/a/other.html"`) {
		t.Errorf("link not rewritten: %q", body)
	}
}

func TestRender_AbsoluteURLsLeftAlone(t *testing.T) {
	r := NewRenderer()
	p := Post{Slug: "a", Body: "[ext](https://example.com/path) and ![](/static/x.png) and [tld](#frag) and [m](mailto:x@example.com)\n"}
	got, err := r.Render(p)
	if err != nil {
		t.Fatal(err)
	}
	body := string(got.BodyHTML)
	for _, want := range []string{
		`href="https://example.com/path"`,
		`src="/static/x.png"`,
		`href="#frag"`,
		`href="mailto:x@example.com"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in body: %s", want, body)
		}
	}
}

func TestRender_RawInlineHTMLPassesThrough(t *testing.T) {
	r := NewRenderer()
	p := Post{Slug: "tabs", Body: "<div class=\"tabs\"><button>One</button></div>\n"}
	got, err := r.Render(p)
	if err != nil {
		t.Fatal(err)
	}
	body := string(got.BodyHTML)
	if !strings.Contains(body, `class="tabs"`) || !strings.Contains(body, "<button>One</button>") {
		t.Errorf("raw HTML did not pass through: %q", body)
	}
}

func TestRender_PerPostTemplate(t *testing.T) {
	dir := t.TempDir()
	tmpl := `<!doctype html><body data-slug="{{.Slug}}"><main>{{.Content}}</main></body>`
	if err := os.WriteFile(filepath.Join(dir, PerPostTemplateFile), []byte(tmpl), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewRenderer()
	p := Post{Slug: "custom", Body: "hello\n", Dir: dir, Date: time.Now()}
	got, err := r.Render(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.PerPostTmpl == nil {
		t.Fatal("expected per-post template, got nil")
	}
	// Render to a buffer to verify it actually composes.
	var b strings.Builder
	err = got.PerPostTmpl.Execute(&b, TemplateData{
		Title:   "T",
		Slug:    p.Slug,
		Date:    p.Date,
		Content: got.BodyHTML,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `data-slug="custom"`) {
		t.Errorf("template substitution failed: %s", b.String())
	}
	if !strings.Contains(b.String(), "hello") {
		t.Errorf("body not embedded: %s", b.String())
	}
}

func TestRender_NoPerPostTemplate(t *testing.T) {
	r := NewRenderer()
	p := Post{Slug: "plain", Body: "x\n", Dir: t.TempDir()}
	got, err := r.Render(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.PerPostTmpl != nil {
		t.Errorf("expected nil PerPostTmpl when no template.html on disk")
	}
}

func TestRender_BrokenPerPostTemplateSurfaced(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, PerPostTemplateFile), []byte(`{{ .UnclosedAction `), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewRenderer()
	_, err := r.Render(Post{Slug: "bad", Body: "", Dir: dir})
	if err == nil {
		t.Fatal("expected error from malformed template")
	}
}

func TestFirstParagraphText(t *testing.T) {
	cases := []struct {
		in   template.HTML
		want string
	}{
		{template.HTML("<p>Hello <strong>world</strong>.</p><p>Second.</p>"), "Hello world."},
		{template.HTML("plain text no paragraph"), "plain text no paragraph"},
		{template.HTML("<p>Unclosed paragraph"), "Unclosed paragraph"},
		{template.HTML(""), ""},
	}
	for _, tc := range cases {
		if got := FirstParagraphText(tc.in); got != tc.want {
			t.Errorf("FirstParagraphText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
