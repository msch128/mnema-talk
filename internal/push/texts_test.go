package push

import "testing"

func TestEveryPushLanguageHasEveryText(t *testing.T) {
	for lang, ts := range texts {
		for key := range texts["en"] {
			if ts[key] == "" {
				t.Errorf("%s: push text %q is missing", lang, key)
			}
		}
		if len(ts) != len(texts["en"]) {
			t.Errorf("%s: has texts English lacks", lang)
		}
	}
}
