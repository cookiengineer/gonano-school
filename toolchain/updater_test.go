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

const updateCategoriesFeed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>List of categories</title>
  <entry><title>wikipedia</title><id>1</id></entry>
  <entry><title>stack_exchange</title><id>2</id></entry>
  <entry><title>other</title><id>3</id></entry>
  <entry><title>broken</title><id>4</id></entry>
</feed>`

func updateEntry(name, language, flavour, filename string, length int64) string {
	return `  <entry>
    <id>urn:` + name + `</id><title>` + name + `</title><language>` + language + `</language>
    <name>` + name + `</name><flavour>` + flavour + `</flavour>
    <link rel="http://opds-spec.org/acquisition/open-access" type="application/x-zim"
          href="https://lb.download.kiwix.org/zim/test/` + filename + `.meta4" length="` + strconv.FormatInt(length, 10) + `"/>
  </entry>`
}

func updaterEntries() map[string][]string {
	return map[string][]string{
		"wikipedia": {
			updateEntry("wikipedia_en_physics", "eng", "maxi", "wikipedia_en_physics_maxi_2026-07.zim", 1000),
			updateEntry("wikipedia_en_physics", "eng", "nopic", "wikipedia_en_physics_nopic_2026-07.zim", 900),
			updateEntry("wikipedia_en_physics", "eng,deu", "nopic", "wikipedia_en_physics_nopic_mul.zim", 800),
			updateEntry("wikipedia_en_football", "eng", "nopic", "wikipedia_en_football_nopic_2026-07.zim", 700),
			updateEntry("wikipedia_en-simple_all", "eng", "nopic", "wikipedia_en-simple_all_nopic_2026-06.zim", 600),
		},
		"stack_exchange": {
			updateEntry("physics.stackexchange.com_en_all", "eng", "", "physics.stackexchange.com_en_all_2026-08.zim", 500),
			updateEntry("softwareengineering.stackexchange.com_en_all", "eng", "", "softwareengineering.stackexchange.com_en_all_2026-08.zim", 400),
			updateEntry("engineering.stackexchange.com_en_all", "eng", "", "engineering.stackexchange.com_en_all_2026-08.zim", 300),
			updateEntry("cooking.stackexchange.com_en_all", "eng", "", "cooking.stackexchange.com_en_all_2026-07.zim", 200),
			updateEntry("ja.stackoverflow.com_mul_all", "eng,deu", "", "ja.stackoverflow.com_mul_all.zim", 100),
		},
		"other": {
			updateEntry("planetmath.org_en_all", "eng", "", "planetmath.org_en_all_2026-08.zim", 200),
			updateEntry("armypubs_en_all", "eng", "", "armypubs_en_all_2024-12.zim", 100),
			updateEntry("unmapped.example.com_en_all", "eng", "", "unmapped.example.com_en_all.zim", 50),
		},
	}
}

func updaterServer(t *testing.T, entries map[string][]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/categories":
			io.WriteString(writer, updateCategoriesFeed)
		case "/entries":
			category := request.URL.Query().Get("category")
			if category == "broken" {
				http.Error(writer, "upstream boom", http.StatusInternalServerError)
				return
			}
			list := entries[category]
			io.WriteString(writer, testFeed(0, len(list), list...))
		default:
			http.NotFound(writer, request)
		}
	}))
}

func updaterClient(server *httptest.Server) *Client {
	return &Client{BaseURL: server.URL, HTTP: server.Client(), Lang: "eng", Count: 100}
}

func TestUpdateBuildsManifest(t *testing.T) {
	server := updaterServer(t, updaterEntries())
	defer server.Close()

	var warnings []string
	manifest, err := Update(context.Background(), UpdateOptions{
		KeepGoing: true,
		Client:    updaterClient(server),
		Warnf:     func(format string, args ...any) { warnings = append(warnings, fmt.Sprintf(format, args...)) },
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if len(warnings) != 1 || !strings.Contains(warnings[0], "broken") {
		t.Fatalf("warnings = %#v, want one about broken", warnings)
	}

	expected := map[string]struct {
		model string
		size  int64
	}{
		"datasets/physics/wikipedia_en_physics_nopic_2026-07.zim":                       {ModelName(CategoryPhysics), 900},
		"datasets/base/wikipedia_en-simple_all_nopic_2026-06.zim":                       {ModelName(CategoryBase), 600},
		"datasets/physics/physics.stackexchange.com_en_all_2026-08.zim":                 {ModelName(CategoryPhysics), 500},
		"datasets/programming/softwareengineering.stackexchange.com_en_all_2026-08.zim": {ModelName(CategoryProgramming), 400},
		"datasets/engineering/engineering.stackexchange.com_en_all_2026-08.zim":         {ModelName(CategoryEngineering), 300},
		"datasets/math/planetmath.org_en_all_2026-08.zim":                               {ModelName(CategoryMath), 200},
		"datasets/cyberstrategy/armypubs_en_all_2024-12.zim":                            {ModelName(CategoryCyberstrategy), 100},
	}
	if len(manifest) != len(expected) {
		t.Fatalf("manifest has %d entries, want %d:\n%#v", len(manifest), len(expected), manifest)
	}
	for key, want := range expected {
		dataset, ok := manifest[key]
		if !ok {
			t.Errorf("missing key %q", key)
			continue
		}
		if dataset.Model != want.model {
			t.Errorf("%s model = %q, want %q", key, dataset.Model, want.model)
		}
		if dataset.Size != want.size {
			t.Errorf("%s size = %d, want %d", key, dataset.Size, want.size)
		}
		if strings.HasSuffix(dataset.URL, ".meta4") {
			t.Errorf("%s url still has .meta4: %q", key, dataset.URL)
		}
		if len(dataset.Categories) != 1 {
			t.Errorf("%s categories = %#v, want exactly one", key, dataset.Categories)
		}
	}
}

func TestUpdateAbortsOnCategoryError(t *testing.T) {
	server := updaterServer(t, updaterEntries())
	defer server.Close()

	if _, err := Update(context.Background(), UpdateOptions{
		Client: updaterClient(server),
	}); err == nil {
		t.Fatal("expected error without KeepGoing")
	}
}

func TestUpdateCategoriesError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	if _, err := Update(context.Background(), UpdateOptions{
		Client: updaterClient(server),
	}); err == nil {
		t.Fatal("expected error when categories fail")
	}
}
