package chat

import "testing"

func TestEverySeedLanguageNamesTheSameCategoriesAndChannels(t *testing.T) {
	base, ok := seedLayouts[seedBaseline]
	if !ok {
		t.Fatalf("no layout for the baseline language %q", seedBaseline)
	}
	for lang, l := range seedLayouts {
		for key := range base.Categories {
			if l.Categories[key] == "" {
				t.Errorf("%s: category %q has no name", lang, key)
			}
		}
		for key := range base.Channels {
			if c := l.Channels[key]; c.Name == "" || c.Topic == "" {
				t.Errorf("%s: channel %q is incomplete: %+v", lang, key, c)
			}
		}
		if len(l.Categories) != len(base.Categories) || len(l.Channels) != len(base.Channels) {
			t.Errorf("%s: has entries the baseline lacks", lang)
		}
	}
}
