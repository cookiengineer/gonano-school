// Package atom parses the Atom/OPDS feeds served by the Kiwix catalog.
//
// The parser deliberately depends only on encoding/xml. Callers (the catalog
// client) treat any structural surprise as an error, because the upstream feed
// is external, unsanitized input that can change shape without notice. The
// decoded shapes live in toolchain/types.
package atom

import (
	"encoding/xml"
	"fmt"
	"io"

	"gonano-school/toolchain/types"
)

// Parse decodes one Atom feed. A document whose root element is not <feed> is
// rejected; this also catches the anti-bot HTML page that some Kiwix hosts
// serve with an HTTP 200 status.
func Parse(reader io.Reader) (*types.Feed, error) {
	decoder := xml.NewDecoder(reader)
	var feed types.Feed
	if err := decoder.Decode(&feed); err != nil {
		return nil, fmt.Errorf("atom: decode: %w", err)
	}
	if feed.XMLName.Local != "feed" {
		return nil, fmt.Errorf("atom: not an Atom feed (root element <%s>)", feed.XMLName.Local)
	}
	return &feed, nil
}
