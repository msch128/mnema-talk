package wiki

import (
	"encoding/json"
	"os"
	"testing"
)

// The web app resolves [[Title]] with the same key (web/src/wiki/lib/wikiTitleKey.ts).
// Its test writes testdata/title_keys.json (WIKI_KEYS_WRITE=1) and checks it; this
// test checks the server's key against the same file without regenerating it.
func TestTitleKeysMatchTheVectors(t *testing.T) {
	raw, err := os.ReadFile("testdata/title_keys.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct{ Title, Key string }
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors) < 30 {
		t.Fatalf("only %d vectors", len(vectors))
	}
	for _, v := range vectors {
		if got := titleKey(v.Title); got != v.Key {
			t.Errorf("titleKey(%+q) = %+q, the web app says %+q", v.Title, got, v.Key)
		}
	}
}

func TestATitleIsStoredComposed(t *testing.T) {
	for in, want := range map[string]string{
		"  Cafe\u0301 ":    "Caf\u00e9",
		"Cafe\u200b\u0301": "Caf\u00e9", // the accent meets its letter once the zero-width space is gone
		"\u212aelvin":      "Kelvin",    // a canonical singleton
		"o\ufb03ce":        "o\ufb03ce", // a compatibility form stays
	} {
		if got, err := cleanTitle(in); err != nil || got != want {
			t.Errorf("cleanTitle(%+q) = %+q, %v; want %+q", in, got, err, want)
		}
	}
}
