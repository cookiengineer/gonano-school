package types

import (
	"encoding/xml"
	"strings"
)

// Link is an Atom <link> element.
type Link struct {
	Rel    string `xml:"rel,attr"`
	Href   string `xml:"href,attr"`
	Type   string `xml:"type,attr"`
	Length int64  `xml:"length,attr"`
}

// Entry is one Atom <entry> of a Kiwix catalog feed.
type Entry struct {
	ID           string `xml:"id"`
	Title        string `xml:"title"`
	Updated      string `xml:"updated"`
	Summary      string `xml:"summary"`
	Language     string `xml:"language"`
	Name         string `xml:"name"`
	Flavour      string `xml:"flavour"`
	Category     string `xml:"category"`
	Tags         string `xml:"tags"`
	ArticleCount int64  `xml:"articleCount"`
	MediaCount   int64  `xml:"mediaCount"`
	Issued       string `xml:"issued"` // dc:issued
	Links        []Link `xml:"link"`
}

// Acquisition returns the open-access ZIM link for the entry. It matches a link
// whose type is an x-zim first, then any link with an acquisition relation.
func (entry Entry) Acquisition() (Link, bool) {
	for _, link := range entry.Links {
		if strings.Contains(link.Type, "x-zim") {
			return link, true
		}
	}
	for _, link := range entry.Links {
		if strings.Contains(link.Rel, "acquisition") {
			return link, true
		}
	}
	return Link{}, false
}

// Feed is a parsed Atom <feed>.
type Feed struct {
	XMLName      xml.Name `xml:"feed"`
	ID           string   `xml:"id"`
	Title        string   `xml:"title"`
	Updated      string   `xml:"updated"`
	TotalResults int      `xml:"totalResults"`
	StartIndex   int      `xml:"startIndex"`
	ItemsPerPage int      `xml:"itemsPerPage"`
	Entries      []Entry  `xml:"entry"`
}
