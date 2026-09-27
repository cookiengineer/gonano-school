package toolchain

import (
	"path"
	"sort"
	"strings"

	"gonano-school/toolchain/types"
)

// Category names are exactly the suffixes of the gonano model names. A
// "gonano-base" model is trained on datasets tagged "base", and so on.
const (
	CategoryBase          = "base"
	CategoryScience       = "science"
	CategoryMath          = "math"
	CategoryChemistry     = "chemistry"
	CategoryPhysics       = "physics"
	CategoryEngineering   = "engineering"
	CategoryMedicine      = "medicine"
	CategoryProgramming   = "programming"
	CategoryCybersecurity = "cybersecurity"
	CategoryHistory       = "history"
	CategoryCyberstrategy = "cyberstrategy"
)

// ModelPrefix is prepended to a category to form a gonano model name.
const ModelPrefix = "gonano-"

// Categories is the closed set of dataset categories (and model suffixes).
var Categories = []string{
	CategoryBase,
	CategoryScience,
	CategoryMath,
	CategoryChemistry,
	CategoryPhysics,
	CategoryEngineering,
	CategoryMedicine,
	CategoryProgramming,
	CategoryCybersecurity,
	CategoryHistory,
	CategoryCyberstrategy,
}

// ModelName returns the gonano model name for a category.
func ModelName(category string) string { return ModelPrefix + category }

// nameRule maps a case-insensitive substring of a dataset name to a category.
type nameRule struct {
	pattern  string
	category string
}

// flavourRank orders ZIM flavours by preference: full text without images
// first, then full text, then the overview-only mini, then unflavoured last.
func flavourRank(flavour string) int {
	switch flavour {
	case "nopic":
		return 0
	case "maxi":
		return 1
	case "mini":
		return 2
	case "":
		return 3
	default:
		return 4
	}
}

// KeepEntry reports whether an entry survives the English / non-video filter.
// Multi-language entries ("eng,deu") are dropped: only pure English is kept.
func KeepEntry(kiwixCategory string, entry types.Entry) bool {
	if !strings.EqualFold(strings.TrimSpace(entry.Language), "eng") {
		return false
	}
	if kiwixCategory == "ted" {
		return false
	}
	return true
}

// SelectFlavour collapses entries that share a logical <name> (which omits the
// flavour and date) into the single best variant, and returns the survivors
// sorted by name for deterministic output.
func SelectFlavour(entries []types.Entry) []types.Entry {
	best := make(map[string]types.Entry)
	for _, entry := range entries {
		name := entry.Name
		if name == "" {
			name = entry.ID
		}
		existing, ok := best[name]
		if !ok || flavourRank(entry.Flavour) < flavourRank(existing.Flavour) {
			best[name] = entry
		}
	}
	names := make([]string, 0, len(best))
	for name := range best {
		names = append(names, name)
	}
	sort.Strings(names)
	selected := make([]types.Entry, 0, len(names))
	for _, name := range names {
		selected = append(selected, best[name])
	}
	return selected
}

// Classify maps a Kiwix category plus archive name to a gonano category. The
// second result is false when the archive must be skipped.
func Classify(kiwixCategory, name, title string) (string, bool) {
	switch kiwixCategory {
	case "wikipedia":
		return classifyWikipedia(name)
	case "wikibooks", "wikiversity", "wiktionary", "wikiquote", "wikisource",
		"wikivoyage", "vikidia", "gutenberg", "mooc":
		return CategoryBase, true
	case "iFixit":
		return CategoryEngineering, true
	case "phet":
		return CategoryPhysics, true
	case "psiram":
		return CategoryScience, true
	case "stack_exchange":
		return classifyStackExchange(name)
	case "other":
		return classifyOther(name)
	default:
		return "", false
	}
}

var wikipediaRules = []nameRule{
	{"wikipedia_en_history", CategoryHistory},
	{"wikipedia_en_astronomy", CategoryScience},
	{"wikipedia_en_molcell", CategoryScience},
	{"wikipedia_en_climate-change", CategoryScience},
	{"wikipedia_en_medicine", CategoryMedicine},
	{"wikipedia_en_physics", CategoryPhysics},
	{"wikipedia_en_chemistry", CategoryChemistry},
	{"wikipedia_en_mathematics", CategoryMath},
	{"wikipedia_en_computer", CategoryProgramming},
	{"wikipedia_en_all", CategoryBase},
	{"wikipedia_en-simple_all", CategoryBase},
	{"wikipedia_en_geography", CategoryBase},
	{"wikipedia_en_sociology", CategoryBase},
	{"wikipedia_en_top1m", CategoryBase},
	{"wikipedia_en_top", CategoryBase},
	{"wikipedia_en_100", CategoryBase},
}

var wikipediaSkip = []string{
	"wikipedia_en_football", "wikipedia_en_basketball", "wikipedia_en_baseball",
	"wikipedia_en_cricket", "wikipedia_en_tennis", "wikipedia_en_golf",
	"wikipedia_en_ice-hockey", "wikipedia_en_movies", "wikipedia_en_indian-cinema",
	"wikipedia_en_nollywood", "wikipedia_en_ray-charles", "wikipedia_en_comics",
	"wikipedia_en_knots",
}

func classifyWikipedia(name string) (string, bool) {
	lower := strings.ToLower(name)
	for _, skip := range wikipediaSkip {
		if strings.Contains(lower, skip) {
			return "", false
		}
	}
	if category, ok := matchName(lower, wikipediaRules); ok {
		return category, true
	}
	return "", false
}

// Stack Exchange entries are named "<host>_<lang>_all", and hostnames contain
// no underscores, so the host is everything before the first underscore. All
// matching below is exact on that host, which avoids substring collisions such
// as "electronics." containing "cs.".
var stackExchangeRules = []nameRule{
	{"security.stackexchange.com", CategoryCybersecurity},
	{"crypto.stackexchange.com", CategoryCybersecurity},
	{"reverseengineering.stackexchange.com", CategoryCybersecurity},
	{"tor.stackexchange.com", CategoryCybersecurity},
	{"physics.stackexchange.com", CategoryPhysics},
	{"chemistry.stackexchange.com", CategoryChemistry},
	{"medicalsciences.stackexchange.com", CategoryMedicine},
	{"history.stackexchange.com", CategoryHistory},
	{"genealogy.stackexchange.com", CategoryHistory},
	{"hsm.stackexchange.com", CategoryHistory},
	{"biology.stackexchange.com", CategoryScience},
	{"astronomy.stackexchange.com", CategoryScience},
	{"earthscience.stackexchange.com", CategoryScience},
	{"space.stackexchange.com", CategoryScience},
	{"scicomp.stackexchange.com", CategoryScience},
	{"mattermodeling.stackexchange.com", CategoryScience},
	{"bioinformatics.stackexchange.com", CategoryScience},
	{"skeptics.stackexchange.com", CategoryScience},
	{"psychology.stackexchange.com", CategoryScience},
	{"math.stackexchange.com", CategoryMath},
	{"mathoverflow.net", CategoryMath},
	{"matheducators.stackexchange.com", CategoryMath},
	{"mathematica.stackexchange.com", CategoryMath},
	{"stats.stackexchange.com", CategoryMath},
	{"or.stackexchange.com", CategoryMath},
	{"proofassistants.stackexchange.com", CategoryMath},
	{"quant.stackexchange.com", CategoryMath},
}

// stackExchangeProgramming lists the technical sites that map to the
// programming model. It is checked before the engineering list so that
// "softwareengineering.stackexchange.com" is not captured by the engineering
// rule. Sites that match neither an explicit category nor this list are
// skipped rather than dumped into programming.
var stackExchangeProgramming = []string{
	"stackoverflow.com", "serverfault.com", "superuser.com", "askubuntu.com",
	"unix.stackexchange.com", "dba.stackexchange.com", "devops.stackexchange.com",
	"softwareengineering.stackexchange.com", "cs.stackexchange.com",
	"cstheory.stackexchange.com", "codereview.stackexchange.com",
	"codegolf.stackexchange.com", "stackapps.com", "sqa.stackexchange.com",
	"datascience.stackexchange.com", "ai.stackexchange.com",
	"genai.stackexchange.com", "computergraphics.stackexchange.com",
	"gamedev.stackexchange.com", "arduino.stackexchange.com",
	"raspberrypi.stackexchange.com", "iot.stackexchange.com",
	"langdev.stackexchange.com", "opensource.stackexchange.com",
	"softwarerecs.stackexchange.com", "webapps.stackexchange.com",
	"webmasters.stackexchange.com", "wordpress.stackexchange.com",
	"drupal.stackexchange.com", "joomla.stackexchange.com",
	"magento.stackexchange.com", "craftcms.stackexchange.com",
	"expressionengine.stackexchange.com", "tridion.stackexchange.com",
	"sitecore.stackexchange.com", "salesforce.stackexchange.com",
	"sharepoint.stackexchange.com", "tex.stackexchange.com",
	"vi.stackexchange.com", "emacs.stackexchange.com",
	"android.stackexchange.com", "apple.stackexchange.com",
	"elementaryos.stackexchange.com", "blender.stackexchange.com",
	"retrocomputing.stackexchange.com",
}

var stackExchangeEngineering = []nameRule{
	{"engineering.stackexchange.com", CategoryEngineering},
	{"electronics.stackexchange.com", CategoryEngineering},
	{"robotics.stackexchange.com", CategoryEngineering},
	{"3dprinting.stackexchange.com", CategoryEngineering},
	{"mechanics.stackexchange.com", CategoryEngineering},
	{"ham.stackexchange.com", CategoryEngineering},
	{"aviation.stackexchange.com", CategoryEngineering},
	{"drones.stackexchange.com", CategoryEngineering},
	{"woodworking.stackexchange.com", CategoryEngineering},
	{"networkengineering.stackexchange.com", CategoryEngineering},
}

func classifyStackExchange(name string) (string, bool) {
	host := strings.ToLower(name)
	if index := strings.Index(host, "_"); index >= 0 {
		host = host[:index]
	}
	if category, ok := matchHost(host, stackExchangeRules); ok {
		return category, true
	}
	for _, pattern := range stackExchangeProgramming {
		if host == pattern {
			return CategoryProgramming, true
		}
	}
	if category, ok := matchHost(host, stackExchangeEngineering); ok {
		return category, true
	}
	return "", false
}

func matchHost(host string, rules []nameRule) (string, bool) {
	for _, rule := range rules {
		if host == rule.pattern {
			return rule.category, true
		}
	}
	return "", false
}

var otherRules = []nameRule{
	{"armypubs", CategoryCyberstrategy},
	{"privacydefence.org_en_opsecbible", CategoryCybersecurity},
	{"anonymousplanet.org", CategoryCybersecurity},
	{"sh1.org", CategoryCybersecurity},
	{"thalesdoc", CategoryCybersecurity},
	{"cloudflare.com_en_learning-center", CategoryCybersecurity},
	{"medlineplus.gov", CategoryMedicine},
	{"nhs.uk_en_medicines", CategoryMedicine},
	{"wwwnc.cdc.gov", CategoryMedicine},
	{"irp.fas.org_en_military-medicine", CategoryMedicine},
	{"womenshistory.si.edu", CategoryHistory},
	{"folgerpedia.folger.edu", CategoryHistory},
	{"apod.nasa.gov", CategoryScience},
	{"encyclopedie-environnement.org", CategoryScience},
	{"planetmath", CategoryMath},
	{"stacks.math.columbia.edu", CategoryMath},
	{"mspeekenbrink", CategoryMath},
	{"learningstatisticswithr", CategoryMath},
	{"learningstatistics", CategoryMath},
	{"ethanweed", CategoryMath},
	{"lost-stats.github.io", CategoryMath},
	{"cd3wdproject", CategoryEngineering},
	{"survivorlibrary", CategoryEngineering},
	{"openwrt.org", CategoryEngineering},
	{"php.net", CategoryProgramming},
	{"docs.python.org", CategoryProgramming},
	{"peps.python", CategoryProgramming},
	{"lua.org", CategoryProgramming},
	{"dart.dev", CategoryProgramming},
	{"getbootstrap", CategoryProgramming},
	{"htdp.org", CategoryProgramming},
	{"opendatastructures", CategoryProgramming},
	{"jeffe.cs.illinois.edu", CategoryProgramming},
	{"gobyexample", CategoryProgramming},
	{"www.mankier.com", CategoryProgramming},
	{"devhints.io", CategoryProgramming},
	{"permacomputing.net", CategoryProgramming},
	{"khanacademy", CategoryBase},
	{"milneopentextbooks", CategoryBase},
	{"booksdash", CategoryBase},
	{"citizensinformation.ie", CategoryBase},
	{"www.ready.gov_en", CategoryBase},
	{"internet-encyclopedia-philosophy", CategoryBase},
	{"based.cooking", CategoryBase},
	{"foss.cooking", CategoryBase},
	{"publicdomainrecipes", CategoryBase},
	{"grimgrains", CategoryBase},
	{"gotquestions", CategoryBase},
	{"openmusictheory", CategoryBase},
	{"music.dalitio.de", CategoryBase},
	{"mutopiaproject", CategoryBase},
	{"chopin.lib.uchicago.edu", CategoryBase},
	{"solar.lowtechmagazine", CategoryBase},
	{"100r.co", CategoryBase},
}

func classifyOther(name string) (string, bool) {
	return matchName(strings.ToLower(name), otherRules)
}

func matchName(lower string, rules []nameRule) (string, bool) {
	for _, rule := range rules {
		if strings.Contains(lower, rule.pattern) {
			return rule.category, true
		}
	}
	return "", false
}

// BuildDataset derives the manifest key and Dataset for one catalog entry.
func BuildDataset(kiwixCategory string, entry types.Entry) (string, types.Dataset, bool) {
	category, ok := Classify(kiwixCategory, entry.Name, entry.Title)
	if !ok {
		return "", types.Dataset{}, false
	}
	link, ok := entry.Acquisition()
	if !ok || link.Href == "" {
		return "", types.Dataset{}, false
	}
	filename := zimFilename(link.Href)
	if filename == "" {
		return "", types.Dataset{}, false
	}
	key := path.Join("datasets", kiwixCategory, filename)
	return key, types.Dataset{
		URL:        strings.TrimSuffix(link.Href, ".meta4"),
		Categories: []string{category},
		Model:      ModelName(category),
		Size:       link.Length,
	}, true
}

// zimFilename extracts "foo.zim" from an acquisition href, tolerating a query
// or fragment and a ".meta4" suffix. It returns "" when the href is not a ZIM.
func zimFilename(href string) string {
	if index := strings.IndexAny(href, "?#"); index >= 0 {
		href = href[:index]
	}
	href = strings.TrimSuffix(href, "/")
	name := href
	if index := strings.LastIndex(href, "/"); index >= 0 {
		name = href[index+1:]
	}
	name = strings.TrimSuffix(name, ".meta4")
	if !strings.HasSuffix(strings.ToLower(name), ".zim") {
		return ""
	}
	return name
}
