package media

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// keySpaces are the storage key prefixes the server writes, and who finds an
// object left there when its row is gone. A prefix the orphan scan does not
// list needs the reason nothing is left behind, or the finding that says it is.
var keySpaces = map[string]string{
	"uploads/":        "orphan scan",
	"avatars/":        "orphan scan",
	"banners/":        "orphan scan",
	"covers/":         "orphan scan",
	"emoji/":          "not scanned: referenced by custom_emoji, not media; a failed delete is logged with its key (internal/emoji/handler.go)",
	"wiki/":           "not scanned: wiki pictures, swept by the wiki itself when it reaches its cap (internal/wiki/images.go)",
	"community/icon/": "not scanned: referenced by instance_settings; a failed delete is logged with its version (internal/onboarding/icon.go)",
	"exports/":        "orphan scan",
}

func TestEveryKeySpaceIsScannedOrHasAReason(t *testing.T) {
	for _, p := range orphanPrefixes {
		if keySpaces[p] != "orphan scan" {
			t.Errorf("the orphan scan lists %s, which keySpaces does not mark as scanned", p)
		}
	}
	for p, why := range keySpaces {
		if (why == "orphan scan") != slices.Contains(orphanPrefixes, p) {
			t.Errorf("%s: keySpaces says %q, orphanPrefixes disagrees", p, why)
		}
	}
}

// The operations guide names the prefixes the daily count covers, and the
// cleanup deletes there: it must name exactly what the code scans. H-L10-04.
func TestTheGuideNamesThePrefixesTheOrphanScanCovers(t *testing.T) {
	raw, err := os.ReadFile("../../doc/operations.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	start := strings.Index(doc, "## Orphaned media")
	end := strings.Index(doc[start+1:], "\n## ")
	if start < 0 || end < 0 {
		t.Fatal("the orphaned media section of doc/operations.md is gone")
	}
	section := doc[start : start+1+end]
	var named []string
	for _, m := range regexp.MustCompile("`([a-z/]+/)`").FindAllStringSubmatch(section, -1) {
		if !slices.Contains(named, m[1]) {
			named = append(named, m[1])
		}
	}
	if len(named) < 3 {
		t.Fatalf("the section names %v: the parser no longer finds the prefixes", named)
	}
	slices.Sort(named)
	scanned := slices.Clone(orphanPrefixes)
	slices.Sort(scanned)
	if !slices.Equal(named, scanned) {
		t.Errorf("the guide names %v, the scan covers %v", named, scanned)
	}
}
