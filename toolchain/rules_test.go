package toolchain

import (
	"testing"

	"gonano-school/toolchain/types"
)

func TestKeepEntry(t *testing.T) {
	cases := []struct {
		category string
		entry    types.Entry
		want     bool
	}{
		{"wikipedia", types.Entry{Language: "eng"}, true},
		{"wikipedia", types.Entry{Language: "eng "}, true},
		{"wikipedia", types.Entry{Language: "ENG"}, true},
		{"wikipedia", types.Entry{Language: "eng,deu"}, false},
		{"wikipedia", types.Entry{Language: "deu"}, false},
		{"wikipedia", types.Entry{Language: ""}, false},
		{"ted", types.Entry{Language: "eng"}, false},
	}
	for _, test := range cases {
		if got := KeepEntry(test.category, test.entry); got != test.want {
			t.Errorf("KeepEntry(%q, %+v) = %v, want %v", test.category, test.entry, got, test.want)
		}
	}
}

func TestSelectFlavourPreference(t *testing.T) {
	entries := []types.Entry{
		{Name: "alpha", Flavour: "mini"},
		{Name: "alpha", Flavour: "maxi"},
		{Name: "alpha", Flavour: "nopic"},
		{Name: "beta", Flavour: "mini"},
		{Name: "beta", Flavour: "maxi"},
		{Name: "gamma", Flavour: "mini"},
		{Name: "delta", Flavour: ""},
		{Name: "epsilon", Flavour: "weird"},
	}
	selected := SelectFlavour(entries)
	got := map[string]string{}
	for _, entry := range selected {
		got[entry.Name] = entry.Flavour
	}
	want := map[string]string{
		"alpha":   "nopic",
		"beta":    "maxi",
		"gamma":   "mini",
		"delta":   "",
		"epsilon": "weird",
	}
	if len(selected) != len(want) {
		t.Fatalf("selected %d entries, want %d", len(selected), len(want))
	}
	for name, flavour := range want {
		if got[name] != flavour {
			t.Errorf("%s flavour = %q, want %q", name, got[name], flavour)
		}
	}
	// Output is sorted by name.
	if selected[0].Name != "alpha" || selected[len(selected)-1].Name != "gamma" {
		t.Errorf("order not deterministic: %+v", selected)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		kiwix    string
		name     string
		category string
		ok       bool
	}{
		{"wikipedia", "wikipedia_en_history", CategoryHistory, true},
		{"wikipedia", "wikipedia_en_physics", CategoryPhysics, true},
		{"wikipedia", "wikipedia_en_chemistry", CategoryChemistry, true},
		{"wikipedia", "wikipedia_en_mathematics", CategoryMath, true},
		{"wikipedia", "wikipedia_en_medicine", CategoryMedicine, true},
		{"wikipedia", "wikipedia_en_computer", CategoryProgramming, true},
		{"wikipedia", "wikipedia_en_molcell", CategoryScience, true},
		{"wikipedia", "wikipedia_en_all", CategoryBase, true},
		{"wikipedia", "wikipedia_en-simple_all", CategoryBase, true},
		{"wikipedia", "wikipedia_en_top1m", CategoryBase, true},
		{"wikipedia", "wikipedia_en_top", CategoryBase, true},
		{"wikipedia", "wikipedia_en_football", "", false},
		{"wikipedia", "wikipedia_en_movies", "", false},
		{"wikipedia", "wikipedia_en_unknown-pack", "", false},

		{"wikibooks", "wikibooks_en_all", CategoryBase, true},
		{"wikiversity", "wikiversity_en_all", CategoryBase, true},
		{"gutenberg", "gutenberg_en_lcc-k", CategoryBase, true},
		{"mooc", "phzh_core-english-one_en", CategoryBase, true},
		{"iFixit", "ifixit_en_all", CategoryEngineering, true},
		{"phet", "phet_en_all", CategoryPhysics, true},
		{"psiram", "psiram_en_all", CategoryScience, true},

		{"stack_exchange", "math.stackexchange.com_en_all", CategoryMath, true},
		{"stack_exchange", "mathoverflow.net_en_all", CategoryMath, true},
		{"stack_exchange", "stats.stackexchange.com_en_all", CategoryMath, true},
		{"stack_exchange", "or.stackexchange.com_en_all", CategoryMath, true},
		{"stack_exchange", "proofassistants.stackexchange.com_en_all", CategoryMath, true},
		{"stack_exchange", "tor.stackexchange.com_en_all", CategoryCybersecurity, true},
		{"stack_exchange", "security.stackexchange.com_en_all", CategoryCybersecurity, true},
		{"stack_exchange", "crypto.stackexchange.com_en_all", CategoryCybersecurity, true},
		{"stack_exchange", "reverseengineering.stackexchange.com_en_all", CategoryCybersecurity, true},
		{"stack_exchange", "physics.stackexchange.com_en_all", CategoryPhysics, true},
		{"stack_exchange", "chemistry.stackexchange.com_en_all", CategoryChemistry, true},
		{"stack_exchange", "medicalsciences.stackexchange.com_en_all", CategoryMedicine, true},
		{"stack_exchange", "history.stackexchange.com_en_all", CategoryHistory, true},
		{"stack_exchange", "biology.stackexchange.com_en_all", CategoryScience, true},
		{"stack_exchange", "earthscience.stackexchange.com_en_all", CategoryScience, true},
		{"stack_exchange", "softwareengineering.stackexchange.com_en_all", CategoryProgramming, true},
		{"stack_exchange", "stackoverflow.com_en_all", CategoryProgramming, true},
		{"stack_exchange", "engineering.stackexchange.com_en_all", CategoryEngineering, true},
		{"stack_exchange", "electronics.stackexchange.com_en_all", CategoryEngineering, true},
		{"stack_exchange", "networkengineering.stackexchange.com_en_all", CategoryEngineering, true},
		{"stack_exchange", "cooking.stackexchange.com_en_all", "", false},
		{"stack_exchange", "pets.stackexchange.com_en_all", "", false},
		{"stack_exchange", "travel.stackexchange.com_en_all", "", false},

		{"other", "armypubs_en_all", CategoryCyberstrategy, true},
		{"other", "planetmath.org_en_all", CategoryMath, true},
		{"other", "stacks.math.columbia.edu_en_all", CategoryMath, true},
		{"other", "docs.python.org_en_all", CategoryProgramming, true},
		{"other", "php.net_en_all", CategoryProgramming, true},
		{"other", "privacydefence.org_en_opsecbible", CategoryCybersecurity, true},
		{"other", "medlineplus.gov_en_all", CategoryMedicine, true},
		{"other", "nhs.uk_en_medicines", CategoryMedicine, true},
		{"other", "khanacademy_en_all", CategoryBase, true},
		{"other", "womenshistory.si.edu_en_all", CategoryHistory, true},
		{"other", "cd3wdproject.org_en_all", CategoryEngineering, true},
		{"other", "openwrt.org_en_all", CategoryEngineering, true},
		{"other", "unmapped.example.com_en_all", "", false},

		{"ted", "ted_mul_worklife", "", false},
	}
	for _, test := range cases {
		category, ok := Classify(test.kiwix, test.name, "")
		if ok != test.ok || category != test.category {
			t.Errorf("Classify(%q, %q) = (%q, %v), want (%q, %v)",
				test.kiwix, test.name, category, ok, test.category, test.ok)
		}
	}
}

func TestBuildDataset(t *testing.T) {
	entry := types.Entry{
		Name:    "wikipedia_en_physics",
		Flavour: "nopic",
		Links: []types.Link{{
			Rel:    "http://opds-spec.org/acquisition/open-access",
			Type:   "application/x-zim",
			Href:   "https://lb.download.kiwix.org/zim/wikipedia/wikipedia_en_physics_nopic_2026-07.zim.meta4",
			Length: 12345,
		}},
	}
	key, dataset, ok := BuildDataset("wikipedia", entry)
	if !ok {
		t.Fatal("BuildDataset returned !ok")
	}
	if key != "datasets/physics/wikipedia_en_physics_nopic_2026-07.zim" {
		t.Errorf("key = %q", key)
	}
	if dataset.Site != "wikipedia" {
		t.Errorf("site = %q, want wikipedia", dataset.Site)
	}
	if dataset.URL != "https://lb.download.kiwix.org/zim/wikipedia/wikipedia_en_physics_nopic_2026-07.zim" {
		t.Errorf("url = %q", dataset.URL)
	}
	if dataset.Model != "gonano-physics" {
		t.Errorf("model = %q", dataset.Model)
	}
	if len(dataset.Categories) != 1 || dataset.Categories[0] != CategoryPhysics {
		t.Errorf("categories = %#v", dataset.Categories)
	}
	if dataset.Size != 12345 {
		t.Errorf("size = %d", dataset.Size)
	}

	if _, _, ok := BuildDataset("wikipedia", types.Entry{Name: "wikipedia_en_physics"}); ok {
		t.Error("expected !ok without an acquisition link")
	}
	badLink := types.Entry{
		Name: "wikipedia_en_physics",
		Links: []types.Link{{
			Rel:  "http://opds-spec.org/acquisition/open-access",
			Type: "text/html",
			Href: "https://example/page.html",
		}},
	}
	if _, _, ok := BuildDataset("wikipedia", badLink); ok {
		t.Error("expected !ok for a non-zim href")
	}
}

func TestZimFilename(t *testing.T) {
	cases := map[string]string{
		"https://lb.download.kiwix.org/zim/gutenberg/foo.zim.meta4": "foo.zim",
		"https://lb.download.kiwix.org/zim/gutenberg/foo.zim":       "foo.zim",
		"https://lb.download.kiwix.org/zim/foo.zim?x=1":             "foo.zim",
		"https://lb.download.kiwix.org/zim/foo.zim#frag":            "foo.zim",
		"/relative/bar.zim":         "bar.zim",
		"https://example/page.html": "",
		"https://example/":          "",
		"":                          "",
	}
	for href, want := range cases {
		if got := zimFilename(href); got != want {
			t.Errorf("zimFilename(%q) = %q, want %q", href, got, want)
		}
	}
}
