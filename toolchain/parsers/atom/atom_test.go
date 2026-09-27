package atom

import (
	"strings"
	"testing"
)

const categoriesFixture = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom"
      xmlns:opds="https://specs.opds.io/opds-1.2">
  <id>aa497008-8dfc-512e-aed8-2e2e027871bd</id>
  <title>List of categories</title>
  <updated>2026-09-27T06:52:36Z</updated>
  <entry>
    <title>gutenberg</title>
    <link rel="subsection" href="/catalog/v2/entries?category=gutenberg"
          type="application/atom+xml;profile=opds-catalog;kind=acquisition"/>
    <id>366e93c4-cb76-99b2-b2b0-898ff12cc739</id>
  </entry>
  <entry>
    <title>wikipedia</title>
    <link rel="subsection" href="/catalog/v2/entries?category=wikipedia"
          type="application/atom+xml;profile=opds-catalog;kind=acquisition"/>
    <id>49cae268-aaa5-f225-5950-1fa1d91293a8</id>
  </entry>
</feed>`

const entriesFixture = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom"
      xmlns:dc="http://purl.org/dc/terms/"
      xmlns:opds="https://specs.opds.io/opds-1.2"
      xmlns:opensearch="http://a9.com/-/spec/opensearch/1.1/">
  <id>eff16bcc-ce49-e18d-0bbc-868851bca6dc</id>
  <title>Filtered Entries (start=0&amp;count=1&amp;lang=eng&amp;category=gutenberg)</title>
  <updated>2026-09-27T06:55:33Z</updated>
  <totalResults>41</totalResults>
  <startIndex>0</startIndex>
  <itemsPerPage>1</itemsPerPage>
  <entry>
    <id>urn:uuid:4a0e3cc9-9974-e341-4457-48f8bb5d176c</id>
    <title>Project Gutenberg Library</title>
    <updated>2026-03-05T00:00:00Z</updated>
    <summary>Law</summary>
    <language>eng</language>
    <name>gutenberg_en_lcc-k</name>
    <flavour></flavour>
    <category>gutenberg</category>
    <tags>_category:gutenberg;gutenberg;_ftindex:no;_pictures:yes;_videos:yes;_details:yes</tags>
    <articleCount>342</articleCount>
    <mediaCount>1003</mediaCount>
    <link rel="http://opds-spec.org/image/thumbnail"
          href="/catalog/v2/illustration/4a0e3cc9-9974-e341-4457-48f8bb5d176c/?size=48"
          type="image/png;width=48;height=48;scale=1"/>
    <link type="text/html" href="https://browse.library.kiwix.org/content/gutenberg_en_lcc-k_2026-03" />
    <author><name>gutenberg.org</name></author>
    <publisher><name>openZIM</name></publisher>
    <dc:issued>2026-03-05T00:00:00Z</dc:issued>
    <link rel="http://opds-spec.org/acquisition/open-access" type="application/x-zim" href="https://lb.download.kiwix.org/zim/gutenberg/gutenberg_en_lcc-k_2026-03.zim.meta4" length="246517760" />
  </entry>
</feed>`

const htmlFixture = `<!DOCTYPE html>
<html lang="en"><head><title>Access confirmation</title></head>
<body><h1>Access confirmation</h1></body></html>`

func TestParseCategoriesFeed(t *testing.T) {
	feed, err := Parse(strings.NewReader(categoriesFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(feed.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(feed.Entries))
	}
	if feed.Entries[0].Title != "gutenberg" || feed.Entries[1].Title != "wikipedia" {
		t.Fatalf("unexpected titles: %q, %q", feed.Entries[0].Title, feed.Entries[1].Title)
	}
}

func TestParseEntriesFeed(t *testing.T) {
	feed, err := Parse(strings.NewReader(entriesFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if feed.TotalResults != 41 || feed.StartIndex != 0 || feed.ItemsPerPage != 1 {
		t.Fatalf("feed paging = %+v", feed)
	}
	if len(feed.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(feed.Entries))
	}
	entry := feed.Entries[0]
	if entry.Name != "gutenberg_en_lcc-k" {
		t.Errorf("name = %q", entry.Name)
	}
	if entry.Language != "eng" {
		t.Errorf("language = %q", entry.Language)
	}
	if entry.ArticleCount != 342 || entry.MediaCount != 1003 {
		t.Errorf("counts = %d/%d", entry.ArticleCount, entry.MediaCount)
	}
	if entry.Issued != "2026-03-05T00:00:00Z" {
		t.Errorf("dc:issued = %q", entry.Issued)
	}
	link, ok := entry.Acquisition()
	if !ok {
		t.Fatal("Acquisition: no link")
	}
	if link.Href != "https://lb.download.kiwix.org/zim/gutenberg/gutenberg_en_lcc-k_2026-03.zim.meta4" {
		t.Errorf("href = %q", link.Href)
	}
	if link.Length != 246517760 {
		t.Errorf("length = %d", link.Length)
	}
}

func TestParseRejectsNonFeedRoot(t *testing.T) {
	if _, err := Parse(strings.NewReader(`<html><body>hi</body></html>`)); err == nil {
		t.Fatal("expected error for non-feed root")
	}
}

func TestParseRejectsAntiBotHTML(t *testing.T) {
	if _, err := Parse(strings.NewReader(htmlFixture)); err == nil {
		t.Fatal("expected error for anti-bot HTML")
	}
}

func TestParseRejectsMalformedXML(t *testing.T) {
	if _, err := Parse(strings.NewReader(`<feed><entry>`)); err == nil {
		t.Fatal("expected error for malformed XML")
	}
}
