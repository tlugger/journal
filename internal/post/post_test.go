package post

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParse_ValidFullPost(t *testing.T) {
	src := []byte(`---
title: Hello
slug: hello-world
date: 2026-05-12
summary: greetings
published: true
---

# Body

Some text.
`)

	p, err := Parse(src, "/v/blog/hello", "/v/blog/hello/index.md", "hello", time.UTC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Title != "Hello" {
		t.Errorf("title = %q", p.Title)
	}
	if p.Slug != "hello-world" {
		t.Errorf("slug = %q", p.Slug)
	}
	// YYYY-MM-DD frontmatter is interpreted as noon in the passed-in
	// location — the load-bearing reason we don't anchor to midnight,
	// which would shift back a day in any reader west of UTC.
	want := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	if !p.Date.Equal(want) {
		t.Errorf("date = %v, want %v", p.Date, want)
	}
	if p.Summary != "greetings" {
		t.Errorf("summary = %q", p.Summary)
	}
	if !strings.Contains(p.Body, "# Body") {
		t.Errorf("body missing heading: %q", p.Body)
	}
	if p.Path != "/v/blog/hello/index.md" || p.Dir != "/v/blog/hello" {
		t.Errorf("path metadata wrong: %+v", p)
	}
}

func TestParse_DraftCases(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "missing_published_field",
			src: `---
title: T
slug: s
date: 2026-01-01
---

body
`,
		},
		{
			name: "explicit_false",
			src: `---
title: T
slug: s
date: 2026-01-01
published: false
---

body
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.src), "/v/blog/x", "/v/blog/x/index.md", "x", time.UTC)
			if !errors.Is(err, ErrDraft) {
				t.Fatalf("expected ErrDraft, got %v", err)
			}
		})
	}
}

func TestParse_SlugFallback(t *testing.T) {
	src := []byte(`---
title: T
date: 2026-01-01
published: true
---

body
`)
	p, err := Parse(src, "/v/blog/my-folder", "/v/blog/my-folder/index.md", "my-folder", time.UTC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Slug != "my-folder" {
		t.Errorf("expected folder-name fallback, got slug=%q", p.Slug)
	}
}

func TestParse_MissingDate(t *testing.T) {
	src := []byte(`---
title: T
slug: s
published: true
---

body
`)
	_, err := Parse(src, "/v/blog/s", "/v/blog/s/index.md", "s", time.UTC)
	if err == nil {
		t.Fatal("expected error for missing date")
	}
	if !strings.Contains(err.Error(), "date") {
		t.Errorf("error should mention date: %v", err)
	}
}

func TestParse_InvalidDate(t *testing.T) {
	src := []byte(`---
title: T
slug: s
date: yesterday
published: true
---

body
`)
	_, err := Parse(src, "/v/blog/s", "/v/blog/s/index.md", "s", time.UTC)
	if err == nil {
		t.Fatal("expected error for bad date")
	}
}

func TestParse_BareDateUsesTimezone(t *testing.T) {
	denver, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Skip("America/Denver tz data not available")
	}
	src := []byte(`---
title: T
slug: s
date: 2026-05-12
published: true
---

body
`)
	p, err := Parse(src, "/v/blog/s", "/v/blog/s/index.md", "s", denver)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Noon in Denver should be the same absolute instant as 18:00 UTC
	// (May 12 in standard MST; 18:00 in MDT is closer to 19:00 UTC,
	// but Denver in May is MDT so noon MDT = 18:00 UTC).
	utc := p.Date.UTC()
	if utc.Year() != 2026 || utc.Month() != time.May || utc.Day() != 12 {
		t.Errorf("UTC calendar date should still be 2026-05-12, got %v", utc)
	}
	if p.Date.Location().String() != "America/Denver" {
		t.Errorf("expected location America/Denver, got %s", p.Date.Location())
	}
	if p.Date.Hour() != 12 {
		t.Errorf("expected hour 12 (noon local), got %d", p.Date.Hour())
	}
}

func TestParse_RFC3339Date(t *testing.T) {
	src := []byte(`---
title: T
slug: s
date: 2026-05-12T14:30:00Z
published: true
---

body
`)
	p, err := Parse(src, "/v/blog/s", "/v/blog/s/index.md", "s", time.UTC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Date.Hour() != 14 || p.Date.Minute() != 30 {
		t.Errorf("RFC3339 time not parsed: %v", p.Date)
	}
}

func TestParse_MalformedYAML(t *testing.T) {
	src := []byte(`---
title: : not yaml
  also broken
---

body
`)
	_, err := Parse(src, "/v/blog/s", "/v/blog/s/index.md", "s", time.UTC)
	if err == nil {
		t.Fatal("expected error for malformed YAML")
	}
}

func TestParse_NoFrontmatter(t *testing.T) {
	src := []byte("just a markdown file with no frontmatter\n")
	_, err := Parse(src, "/v/blog/x", "/v/blog/x/index.md", "x", time.UTC)
	if !errors.Is(err, ErrNoFrontmatter) {
		t.Fatalf("expected ErrNoFrontmatter, got %v", err)
	}
}

func TestParse_NoClosingFence(t *testing.T) {
	src := []byte(`---
title: T
slug: s
date: 2026-01-01
published: true

body without closing fence
`)
	_, err := Parse(src, "/v/blog/s", "/v/blog/s/index.md", "s", time.UTC)
	if err == nil {
		t.Fatal("expected error for missing closing fence")
	}
}

func TestParse_CRLFNormalized(t *testing.T) {
	src := []byte("---\r\ntitle: T\r\nslug: s\r\ndate: 2026-01-01\r\npublished: true\r\n---\r\n\r\nbody\r\n")
	p, err := Parse(src, "/v/blog/s", "/v/blog/s/index.md", "s", time.UTC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Slug != "s" {
		t.Errorf("CRLF input: slug=%q", p.Slug)
	}
}
