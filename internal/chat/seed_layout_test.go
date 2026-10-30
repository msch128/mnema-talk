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

// The seeded names are written through the same validation as a name typed by
// hand, so a language file can never seed a name the rename route would refuse.
func TestSeedLayoutNamesPassChannelValidation(t *testing.T) {
	for lang, l := range seedLayouts {
		for key, name := range l.Categories {
			if _, err := validateOptionalName(&name); err != nil {
				t.Errorf("%s: category %q: %v", lang, key, err)
			}
		}
		for key, c := range l.Channels {
			if _, err := validateOptionalName(&c.Name); err != nil {
				t.Errorf("%s: channel %q name: %v", lang, key, err)
			}
			if _, err := validateOptionalTopic(&c.Topic); err != nil {
				t.Errorf("%s: channel %q topic: %v", lang, key, err)
			}
		}
	}
}
