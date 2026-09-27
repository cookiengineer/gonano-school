package toolchain

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

const testCategoriesFeed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>List of categories</title>
  <entry><title>wikipedia</title><id>1</id></entry>
  <entry><title>gutenberg</title><id>2</id></entry>
</feed>`

const testHTMLPage = `<!DOCTYPE html><html><body><h1>Access confirmation</h1></body></html>`

func testAcquisitionEntry(name, flavour string, index int) string {
	return fmt.Sprintf(`<entry>
  <id>urn:%s</id><title>%s</title><language>eng</language>
  <name>%s</name><flavour>%s</flavour>
  <link rel="http://opds-spec.org/acquisition/open-access" type="application/x-zim"
        href="https://example/%s_%d.zim.meta4" length="100"/>
</entry>`, name, name, name, flavour, name, index)
}

func testFeed(start, total int, entries ...string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Entries</title>
  <totalResults>` + strconv.Itoa(total) + `</totalResults>
  <startIndex>` + strconv.Itoa(start) + `</startIndex>
  <itemsPerPage>` + strconv.Itoa(len(entries)) + `</itemsPerPage>` +
		strings.Join(entries, "") + `</feed>`
}

func TestCatalogCategories(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/categories" {
			http.NotFound(writer, request)
			return
		}
		io.WriteString(writer, testCategoriesFeed)
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, HTTP: server.Client()}
	categories, err := client.Categories(context.Background())
	if err != nil {
		t.Fatalf("Categories: %v", err)
	}
	if len(categories) != 2 || categories[0] != "wikipedia" || categories[1] != "gutenberg" {
		t.Fatalf("categories = %#v", categories)
	}
}

func TestCatalogCategoriesRejectsHTML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		io.WriteString(writer, testHTMLPage)
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, HTTP: server.Client()}
	if _, err := client.Categories(context.Background()); err == nil {
		t.Fatal("expected error for anti-bot HTML body")
	}
}

func TestCatalogEntriesPagination(t *testing.T) {
	names := []string{"a", "b", "c", "d", "e"}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		start, _ := strconv.Atoi(request.URL.Query().Get("start"))
		var entries []string
		for index := start; index < len(names) && index < start+2; index++ {
			entries = append(entries, testAcquisitionEntry(names[index], "nopic", index))
		}
		io.WriteString(writer, testFeed(start, len(names), entries...))
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, HTTP: server.Client(), Count: 2}
	entries, err := client.Entries(context.Background(), "wikipedia")
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != len(names) {
		t.Fatalf("entries = %d, want %d", len(entries), len(names))
	}
	for index, entry := range entries {
		if entry.Name != names[index] {
			t.Fatalf("entry[%d].Name = %q, want %q", index, entry.Name, names[index])
		}
	}
	if requests < 3 {
		t.Fatalf("expected at least 3 page requests, got %d", requests)
	}
}

func TestCatalogEntriesStopsWhenStartNotHonored(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		io.WriteString(writer, testFeed(0, 1000,
			testAcquisitionEntry("a", "nopic", 0),
			testAcquisitionEntry("b", "nopic", 0)))
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, HTTP: server.Client(), Count: 2}
	entries, err := client.Entries(context.Background(), "wikipedia")
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	// The guard stops after the second page; without it the test would loop.
	if requests != 2 {
		t.Fatalf("requests = %d, want 2 (startIndex guard)", requests)
	}
	if len(entries) != 4 {
		t.Fatalf("entries = %d, want 4", len(entries))
	}
}

func TestCatalogEntriesHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, HTTP: server.Client()}
	if _, err := client.Entries(context.Background(), "wikipedia"); err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}

func TestCatalogEntriesEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		io.WriteString(writer, testFeed(0, 0))
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, HTTP: server.Client()}
	entries, err := client.Entries(context.Background(), "wikipedia")
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %d, want 0", len(entries))
	}
}
