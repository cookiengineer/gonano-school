package toolchain

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gonano-school/toolchain/parsers/atom"
	"gonano-school/toolchain/types"
)

// DefaultCatalogBase is the working Kiwix OPDS/Atom catalog. The browser host
// browse.library.kiwix.org serves an anti-bot HTML page to plain clients and
// must not be used. library.kiwix.org 301-redirects here.
const DefaultCatalogBase = "https://opds.library.kiwix.org/catalog/v2"

// DefaultUserAgent identifies gonano-school to the catalog.
const DefaultUserAgent = "gonano-school/0.1 (+https://github.com/cookiengineer/gonano-school)"

// maxCatalogPages bounds pagination so a misbehaving server cannot loop forever.
const maxCatalogPages = 1000

// Client talks to the Kiwix catalog.
type Client struct {
	BaseURL   string
	Lang      string
	Count     int
	HTTP      *http.Client
	userAgent string
}

// NewClient returns a client with sane defaults.
func NewClient() *Client {
	return &Client{
		BaseURL: DefaultCatalogBase,
		Lang:    "eng",
		Count:   100,
		HTTP:    &http.Client{Timeout: 90 * time.Second},
	}
}

func (client *Client) baseURL() string {
	if client.BaseURL == "" {
		return DefaultCatalogBase
	}
	return strings.TrimRight(client.BaseURL, "/")
}

func (client *Client) httpClient() *http.Client {
	if client.HTTP == nil {
		return http.DefaultClient
	}
	return client.HTTP
}

func (client *Client) agent() string {
	if client.userAgent != "" {
		return client.userAgent
	}
	return DefaultUserAgent
}

// fetch GETs a catalog path and parses the Atom response.
func (client *Client) fetch(ctx context.Context, requestPath string) (*types.Feed, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.baseURL()+requestPath, nil)
	if err != nil {
		return nil, fmt.Errorf("catalog: build request %s: %w", requestPath, err)
	}
	request.Header.Set("User-Agent", client.agent())
	request.Header.Set("Accept", "application/atom+xml, application/xml;q=0.9, text/xml;q=0.8, */*;q=0.1")

	response, err := client.httpClient().Do(request)
	if err != nil {
		return nil, fmt.Errorf("catalog: GET %s: %w", requestPath, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 256))
		return nil, fmt.Errorf("catalog: GET %s: status %d: %s", requestPath, response.StatusCode, strings.TrimSpace(string(body)))
	}

	feed, err := atom.Parse(response.Body)
	if err != nil {
		return nil, fmt.Errorf("catalog: GET %s: %w", requestPath, err)
	}
	return feed, nil
}

// Categories returns the catalog category names.
func (client *Client) Categories(ctx context.Context) ([]string, error) {
	feed, err := client.fetch(ctx, "/categories")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(feed.Entries))
	for _, entry := range feed.Entries {
		if title := strings.TrimSpace(entry.Title); title != "" {
			names = append(names, title)
		}
	}
	return names, nil
}

// Entries returns every entry of one category, paginating by totalResults. It
// stops when the server does not advance startIndex, which guards against a
// server that ignores the start parameter.
func (client *Client) Entries(ctx context.Context, category string) ([]types.Entry, error) {
	lang := client.Lang
	if lang == "" {
		lang = "eng"
	}
	count := client.Count
	if count <= 0 {
		count = 100
	}

	start := 0
	var all []types.Entry
	for page := 0; page < maxCatalogPages; page++ {
		requestPath := fmt.Sprintf("/entries?start=%d&count=%d&lang=%s&category=%s",
			start, count, url.QueryEscape(lang), url.QueryEscape(category))
		feed, err := client.fetch(ctx, requestPath)
		if err != nil {
			return nil, err
		}
		if len(feed.Entries) == 0 {
			break
		}
		all = append(all, feed.Entries...)

		next := start + len(feed.Entries)
		if feed.TotalResults > 0 && next >= feed.TotalResults {
			break
		}
		if feed.StartIndex != start {
			// The server ignored `start`; requesting again would repeat a page.
			break
		}
		start = next
	}
	return all, nil
}
