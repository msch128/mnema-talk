package onboarding

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNextRulesVersion(t *testing.T) {
	cases := []struct {
		name    string
		current int
		in      SettingsInput
		want    int
	}{
		{"first rules start at 1", 0, SettingsInput{RulesText: "be kind"}, 1},
		{"edit keeps the version", 3, SettingsInput{RulesText: "be kinder"}, 3},
		{"re-acceptance raises it", 3, SettingsInput{RulesText: "be kinder", RequireReacceptance: true}, 4},
		{"empty rules keep it", 3, SettingsInput{RequireReacceptance: true}, 3},
		{"no rules ever", 0, SettingsInput{CommunityName: "x"}, 0},
	}
	for _, tc := range cases {
		if got := nextRulesVersion(tc.current, tc.in); got != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestCleanSettingsBoundsTheTexts(t *testing.T) {
	ok := SettingsInput{CommunityName: "  Mnema  ", WelcomeText: "hi", RulesText: "r"}
	if err := cleanSettings(&ok); err != nil || ok.CommunityName != "Mnema" {
		t.Fatalf("valid input: %v %q", err, ok.CommunityName)
	}
	for name, in := range map[string]SettingsInput{
		"name":    {CommunityName: strings.Repeat("a", maxCommunityName+1)},
		"welcome": {WelcomeText: strings.Repeat("a", maxWelcomeText+1)},
		"rules":   {RulesText: strings.Repeat("a", maxRulesText+1)},
		"nul":     {RulesText: "a\x00b"},
	} {
		if err := cleanSettings(&in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestTipIDsAreValidated(t *testing.T) {
	if err := validTips([]string{"voice-tip", "a.b_c"}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "Upper", "../x", strings.Repeat("a", 41), "a b"} {
		if validTips([]string{bad}) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if validTips(make([]string, 33)) == nil {
		t.Error("33 tips accepted")
	}
}

func TestMergeTipsKeepsUnknownKeysAndNeverForgets(t *testing.T) {
	done := true
	out, err := mergeTips([]byte(`{"dismissed":["a"],"other":{"x":1}}`), TipsInput{DismissedTips: []string{"b", "a"}, WelcomeDone: &done})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if string(doc["dismissed"]) != `["a","b"]` || string(doc["welcome_done"]) != "true" || string(doc["other"]) != `{"x":1}` {
		t.Fatalf("merged: %s", out)
	}
	// Garbage in the column does not break the next write.
	if _, err := mergeTips([]byte(`not json`), TipsInput{DismissedTips: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	many := make([]string, 0, 40)
	for i := range 40 {
		many = append(many, "t"+strings.Repeat("a", i%5)+string(rune('a'+i%26))+string(rune('a'+i/26)))
	}
	big, err := mergeTips(nil, TipsInput{DismissedTips: many})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mergeTips(big, TipsInput{DismissedTips: many[:0]}); err != nil {
		t.Fatal(err)
	}
}

func TestMergeTipsCapsWhatIsStored(t *testing.T) {
	var stored []string
	for i := range maxStoredTips {
		stored = append(stored, "tip-"+strings.Repeat("x", i/10)+string(rune('a'+i%10)))
	}
	raw, _ := json.Marshal(map[string]any{"dismissed": stored})
	if _, err := mergeTips(raw, TipsInput{DismissedTips: []string{"one-more"}}); err == nil {
		t.Fatal("stored list grew past the cap")
	}
}
