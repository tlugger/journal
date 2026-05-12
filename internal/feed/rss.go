// Package feed generates the RSS 2.0 document served at /rss.xml.
//
// The feed is summary-only by design: each item carries a short description
// and a link back to the post on the live site. We never inline full
// rendered HTML — custom-template posts wouldn't render right in a reader
// without their CSS, and the full-text mirror feels off for a personal blog.
package feed

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"net/url"
	"sort"
	"time"
)

// MaxItems caps the feed length so a long-running site doesn't ship a 10MB
// XML document to every poll.
const MaxItems = 50

// Item is the minimum a feed item needs from the caller's perspective.
type Item struct {
	Title       string
	Slug        string
	Date        time.Time
	Description string // pre-stripped summary text, plain-text or light HTML
}

// Config is the channel-level metadata for the feed.
type Config struct {
	Title       string
	Link        string // canonical site URL, e.g. "https://blog.tylerkno.ws"
	Description string
	SelfURL     string // canonical RSS URL, e.g. "https://blog.tylerkno.ws/rss.xml"
	Author      string
}

// Build returns the bytes of a valid RSS 2.0 document. Items are sorted
// newest-first and capped at MaxItems. The caller is responsible for
// supplying clean descriptions — Build will only XML-escape, not strip HTML.
func Build(cfg Config, items []Item, now time.Time) ([]byte, error) {
	if cfg.Link == "" {
		return nil, fmt.Errorf("feed config: Link is required")
	}
	if _, err := url.Parse(cfg.Link); err != nil {
		return nil, fmt.Errorf("feed config: invalid Link %q: %w", cfg.Link, err)
	}

	sorted := append([]Item(nil), items...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Date.Equal(sorted[j].Date) {
			return sorted[i].Slug < sorted[j].Slug
		}
		return sorted[i].Date.After(sorted[j].Date)
	})
	if len(sorted) > MaxItems {
		sorted = sorted[:MaxItems]
	}

	doc := rssDoc{
		Version: "2.0",
		Atom:    "http://www.w3.org/2005/Atom",
		Channel: rssChannel{
			Title:         cfg.Title,
			Link:          cfg.Link,
			Description:   cfg.Description,
			LastBuildDate: now.UTC().Format(time.RFC1123Z),
			AtomLink: atomLink{
				Href: cfg.SelfURL,
				Rel:  "self",
				Type: "application/rss+xml",
			},
			Items: make([]rssItem, 0, len(sorted)),
		},
	}
	for _, it := range sorted {
		postURL, err := joinURL(cfg.Link, "/posts/"+it.Slug)
		if err != nil {
			return nil, err
		}
		doc.Channel.Items = append(doc.Channel.Items, rssItem{
			Title:       it.Title,
			Link:        postURL,
			GUID:        postURL,
			Description: it.Description,
			PubDate:     it.Date.UTC().Format(time.RFC1123Z),
			Author:      cfg.Author,
		})
	}

	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func joinURL(base, suffix string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	ref, err := url.Parse(suffix)
	if err != nil {
		return "", err
	}
	return u.ResolveReference(ref).String(), nil
}

// XML structs. RSS 2.0 with an atom:link rel=self so feed validators are happy.

type rssDoc struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Atom    string     `xml:"xmlns:atom,attr"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title         string    `xml:"title"`
	Link          string    `xml:"link"`
	Description   string    `xml:"description"`
	LastBuildDate string    `xml:"lastBuildDate,omitempty"`
	AtomLink      atomLink  `xml:"atom:link"`
	Items         []rssItem `xml:"item"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	GUID        string `xml:"guid"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
	Author      string `xml:"author,omitempty"`
}
