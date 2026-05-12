package feed

import (
	"encoding/xml"
	"fmt"
	"strings"
	"testing"
	"time"
)

// minimalRSS is just enough of the schema to verify our output round-trips.
// The atom:link rel=self element is asserted via raw-string matching in
// TestBuild_ValidRSS rather than here, because encoding/xml's unmarshal
// can't distinguish two same-local-name elements without explicit
// namespace handling that isn't worth modeling for tests.
type minimalRSS struct {
	XMLName xml.Name `xml:"rss"`
	Version string   `xml:"version,attr"`
	Channel struct {
		Title       string `xml:"title"`
		Link        string `xml:"link"`
		Description string `xml:"description"`
		Items       []struct {
			Title       string `xml:"title"`
			Link        string `xml:"link"`
			GUID        string `xml:"guid"`
			Description string `xml:"description"`
			PubDate     string `xml:"pubDate"`
		} `xml:"item"`
	} `xml:"channel"`
}

func standardCfg() Config {
	return Config{
		Title:       "Tyler's blog",
		Link:        "https://blog.tylerkno.ws",
		Description: "blog",
		SelfURL:     "https://blog.tylerkno.ws/rss.xml",
		Author:      "tyler@example.com (Tyler)",
	}
}

func TestBuild_ValidRSS(t *testing.T) {
	out, err := Build(standardCfg(), []Item{
		{Title: "Post one", Slug: "post-one", Date: mustDate("2026-05-10"), Description: "first summary"},
	}, mustDate("2026-05-12"))
	if err != nil {
		t.Fatal(err)
	}

	var doc minimalRSS
	if err := xml.Unmarshal(out, &doc); err != nil {
		t.Fatalf("output is not valid XML/RSS: %v\n%s", err, out)
	}
	if doc.Version != "2.0" {
		t.Errorf("version = %q, want 2.0", doc.Version)
	}
	if doc.Channel.Title != "Tyler's blog" {
		t.Errorf("title = %q", doc.Channel.Title)
	}
	if len(doc.Channel.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(doc.Channel.Items))
	}
	if doc.Channel.Items[0].Link != "https://blog.tylerkno.ws/posts/post-one" {
		t.Errorf("item link = %q", doc.Channel.Items[0].Link)
	}
	if !strings.Contains(string(out), `rel="self"`) {
		t.Errorf("missing atom:link rel=self")
	}
	if !strings.Contains(string(out), `xmlns:atom="http://www.w3.org/2005/Atom"`) {
		t.Errorf("missing atom xmlns declaration")
	}
}

func TestBuild_SortsNewestFirst(t *testing.T) {
	items := []Item{
		{Title: "older", Slug: "older", Date: mustDate("2026-05-01")},
		{Title: "newer", Slug: "newer", Date: mustDate("2026-05-10")},
		{Title: "middle", Slug: "middle", Date: mustDate("2026-05-05")},
	}
	out, err := Build(standardCfg(), items, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var doc minimalRSS
	if err := xml.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	wantOrder := []string{"newer", "middle", "older"}
	if len(doc.Channel.Items) != 3 {
		t.Fatalf("got %d items", len(doc.Channel.Items))
	}
	for i, want := range wantOrder {
		if doc.Channel.Items[i].Title != want {
			t.Errorf("items[%d] = %q, want %q", i, doc.Channel.Items[i].Title, want)
		}
	}
}

func TestBuild_CapsAtMaxItems(t *testing.T) {
	items := make([]Item, 75)
	for i := range items {
		items[i] = Item{
			Title: fmt.Sprintf("post-%02d", i),
			Slug:  fmt.Sprintf("post-%02d", i),
			Date:  mustDate("2026-05-01").Add(time.Duration(i) * 24 * time.Hour),
		}
	}
	out, err := Build(standardCfg(), items, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var doc minimalRSS
	if err := xml.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Channel.Items) != MaxItems {
		t.Errorf("got %d items, want cap of %d", len(doc.Channel.Items), MaxItems)
	}
}

func TestBuild_DescriptionEscaped(t *testing.T) {
	out, err := Build(standardCfg(), []Item{
		{Title: "p", Slug: "p", Date: mustDate("2026-05-01"), Description: "<script>alert(1)</script>"},
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "<script>") {
		t.Errorf("script tag not escaped: %s", s)
	}
	if !strings.Contains(s, "&lt;script&gt;") {
		t.Errorf("expected escaped script tag, got: %s", s)
	}
}

func TestBuild_RejectsEmptyLink(t *testing.T) {
	_, err := Build(Config{}, []Item{
		{Title: "x", Slug: "x", Date: time.Now()},
	}, time.Now())
	if err == nil {
		t.Fatal("expected error for missing Link")
	}
}

func TestBuild_UTCPubDate(t *testing.T) {
	// Date in a non-UTC zone — RSS 2.0 RFC1123Z output should still resolve cleanly.
	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Skip("America/Los_Angeles tz data not available")
	}
	d := time.Date(2026, 5, 10, 8, 0, 0, 0, la)
	out, err := Build(standardCfg(), []Item{
		{Title: "p", Slug: "p", Date: d},
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var doc minimalRSS
	if err := xml.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	parsed, err := time.Parse(time.RFC1123Z, doc.Channel.Items[0].PubDate)
	if err != nil {
		t.Fatalf("pubDate not RFC1123Z: %q (%v)", doc.Channel.Items[0].PubDate, err)
	}
	if !parsed.Equal(d) {
		t.Errorf("round-trip mismatch: got %v, want %v", parsed, d)
	}
}

func mustDate(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}
