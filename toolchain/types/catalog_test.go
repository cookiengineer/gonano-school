package types

import "testing"

func TestEntryAcquisitionPrefersXZim(t *testing.T) {
	entry := Entry{Links: []Link{
		{Rel: "http://opds-spec.org/image/thumbnail", Href: "/img", Type: "image/png"},
		{Rel: "alternate", Href: "https://example/page.html", Type: "application/x-zim"},
		{Rel: "http://opds-spec.org/acquisition/open-access", Href: "https://example/foo.zim", Type: "application/octet-stream"},
	}}
	link, ok := entry.Acquisition()
	if !ok || link.Href != "https://example/page.html" {
		t.Fatalf("Acquisition = %+v, %v", link, ok)
	}
}

func TestEntryAcquisitionFallsBackToRelation(t *testing.T) {
	entry := Entry{Links: []Link{
		{Rel: "http://opds-spec.org/image/thumbnail", Href: "/img", Type: "image/png"},
		{Rel: "http://opds-spec.org/acquisition/open-access", Href: "https://example/foo.zim", Type: "application/octet-stream"},
	}}
	link, ok := entry.Acquisition()
	if !ok || link.Href != "https://example/foo.zim" {
		t.Fatalf("Acquisition = %+v, %v", link, ok)
	}
}

func TestEntryAcquisitionMissing(t *testing.T) {
	entry := Entry{Links: []Link{{Rel: "alternate", Href: "/x", Type: "text/html"}}}
	if _, ok := entry.Acquisition(); ok {
		t.Fatal("expected no acquisition link")
	}
	if _, ok := (Entry{}).Acquisition(); ok {
		t.Fatal("expected no acquisition link on empty entry")
	}
}

func TestKeyCategory(t *testing.T) {
	if category := KeyCategory("datasets/base/foo.zim"); category != "base" {
		t.Fatalf("KeyCategory = %q, want base", category)
	}
	for _, key := range []string{"", "foo.zim", "datasets/foo.zim", "a/b/c/d.zim"} {
		if category := KeyCategory(key); category != "" {
			t.Errorf("KeyCategory(%q) = %q, want empty", key, category)
		}
	}
}

func TestKeyFilename(t *testing.T) {
	if name := KeyFilename("datasets/base/foo.zim"); name != "foo.zim" {
		t.Fatalf("KeyFilename = %q, want foo.zim", name)
	}
	for _, key := range []string{"", "foo.zim", "datasets/foo.zim", "a/b/c/d.zim", "datasets/a/b/c.zim"} {
		if name := KeyFilename(key); name != "" {
			t.Errorf("KeyFilename(%q) = %q, want empty", key, name)
		}
	}
}

func TestManifestHelpers(t *testing.T) {
	manifest := Manifest{
		"datasets/a/x.zim": {Categories: []string{"base"}, Size: 2},
		"datasets/b/y.zim": {Categories: []string{"base", "physics"}, Size: 3},
	}
	if manifest.TotalSize() != 5 {
		t.Fatalf("TotalSize = %d, want 5", manifest.TotalSize())
	}
	set := manifest.CategorySet()
	if len(set) != 2 || !set["base"] || !set["physics"] {
		t.Fatalf("CategorySet = %#v", set)
	}
	keys := manifest.Keys()
	if len(keys) != 2 || keys[0] != "datasets/a/x.zim" || keys[1] != "datasets/b/y.zim" {
		t.Fatalf("Keys = %#v", keys)
	}
}
