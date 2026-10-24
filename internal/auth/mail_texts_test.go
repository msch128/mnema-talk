package auth

import "testing"

func TestEveryMailLanguageHasEveryText(t *testing.T) {
	for lang, texts := range mailTexts {
		for key := range mailTexts["en"] {
			if texts[key] == "" {
				t.Errorf("%s: mail text %q is missing", lang, key)
			}
		}
		if len(texts) != len(mailTexts["en"]) {
			t.Errorf("%s: has texts English lacks", lang)
		}
	}
}
