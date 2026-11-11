package discordimport

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
)

// The web app translates hints by code; a code the server can emit but the web
// list does not know would silently show English text, and the reverse leaves
// dead translations. The two lists must be the same set.
func TestWebAndServerKnowTheSameHintCodes(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "admin", "lib", "discordImportHints.ts"))
	if err != nil {
		t.Skipf("web sources not available: %v", err)
	}
	for name, server := range map[string][]string{"IMPORT_NOTE_CODES": NoteCodes, "IMPORT_SKIP_CODES": SkipCodes} {
		list := regexp.MustCompile(`(?s)` + name + ` = \[(.*?)\] as const`).FindSubmatch(src)
		if list == nil {
			t.Fatalf("%s not found in discordImportHints.ts", name)
		}
		var web []string
		for _, m := range regexp.MustCompile(`'([a-z_]+)'`).FindAllSubmatch(list[1], -1) {
			web = append(web, string(m[1]))
		}
		a, b := slices.Sorted(slices.Values(web)), slices.Sorted(slices.Values(server))
		if !slices.Equal(a, b) {
			t.Errorf("%s: web has %v, server emits %v", name, a, b)
		}
	}
}
