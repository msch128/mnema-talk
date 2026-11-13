package mnema

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"runtime/debug"
	"strings"
	"testing"
)

func TestBuildersProduceDiscordsJSON(t *testing.T) {
	rows := []Component{
		ActionRow(SuccessButton("deploy:v2.4", "Deploy"), SecondaryButton("cancel", "Cancel"), LinkButton("https://example.org/changes", "Changelog")),
		ActionRow(StringSelect{CustomID: "env", Placeholder: "Environment", Options: []SelectOption{{Label: "Staging", Value: "staging"}, {Label: "Production", Value: "prod", Default: true}}}),
	}
	b, _ := json.Marshal(rows)
	want := `[{"type":1,"components":[{"type":2,"custom_id":"deploy:v2.4","style":3,"label":"Deploy"},{"type":2,"custom_id":"cancel","style":2,"label":"Cancel"},{"type":2,"style":5,"label":"Changelog","url":"https://example.org/changes"}]},` +
		`{"type":1,"components":[{"type":3,"custom_id":"env","options":[{"label":"Staging","value":"staging"},{"label":"Production","value":"prod","default":true}],"placeholder":"Environment"}]}]`
	if string(b) != want {
		t.Fatalf("json = %s\nwant %s", b, want)
	}
	if err := ValidateComponents(rows); err != nil {
		t.Fatal(err)
	}
}

func TestEntitySelectsCarryTheirTypes(t *testing.T) {
	for want, c := range map[ComponentType]ComponentBuilder{
		ComponentUserSelect: UserSelect{CustomID: "u"}, ComponentRoleSelect: RoleSelect{CustomID: "r"}, ComponentChannelSelect: ChannelSelect{CustomID: "c", MinValues: Ptr(0), MaxValues: Ptr(3)},
	} {
		if got := c.Component(); got.Type != want || got.Validate() != nil {
			t.Errorf("type %d: %+v, %v", want, got, got.Validate())
		}
	}
}

func TestButtonsDefaultToPrimaryOrLinkAndCarryAnEmoji(t *testing.T) {
	if c := (Button{CustomID: "a", Emoji: "👍"}).Component(); c.Style != int(ButtonPrimary) || c.Emoji.Name != "👍" || c.Validate() != nil {
		t.Fatalf("button = %+v", c)
	}
	if c := (Button{URL: "https://example.org", Label: "x"}).Component(); c.Style != int(ButtonLink) {
		t.Fatalf("link = %+v", c)
	}
}

func TestComponentLimitsAreEnforced(t *testing.T) {
	long := strings.Repeat("x", 101)
	opts := func(n int) []SelectOption {
		var o []SelectOption
		for i := range n {
			o = append(o, SelectOption{Label: "l", Value: string(rune('a' + i))})
		}
		return o
	}
	six := []ComponentBuilder{}
	for i := range 6 {
		six = append(six, PrimaryButton(string(rune('a'+i)), "b"))
	}
	bad := map[string][]Component{
		"six rows":             {ActionRow(PrimaryButton("a", "b")), ActionRow(PrimaryButton("c", "b")), ActionRow(PrimaryButton("d", "b")), ActionRow(PrimaryButton("e", "b")), ActionRow(PrimaryButton("f", "b")), ActionRow(PrimaryButton("g", "b"))},
		"six buttons":          {ActionRow(six...)},
		"empty row":            {ActionRow()},
		"not a row":            {PrimaryButton("a", "b").Component()},
		"select with a button": {ActionRow(StringSelect{CustomID: "s", Options: opts(1)}, PrimaryButton("a", "b"))},
		"long custom id":       {ActionRow(PrimaryButton(long, "b"))},
		"no custom id":         {ActionRow(PrimaryButton("", "b"))},
		"long label":           {ActionRow(PrimaryButton("a", strings.Repeat("x", 81)))},
		"no label or emoji":    {ActionRow(PrimaryButton("a", ""))},
		"link with id":         {ActionRow(Button{Style: ButtonLink, URL: "https://x.org", CustomID: "a", Label: "x"})},
		"ftp link":             {ActionRow(LinkButton("ftp://x.org", "x"))},
		"duplicate ids":        {ActionRow(PrimaryButton("a", "b"), PrimaryButton("a", "c"))},
		"no options":           {ActionRow(StringSelect{CustomID: "s"})},
		"26 options":           {ActionRow(StringSelect{CustomID: "s", Options: opts(26)})},
		"duplicate values":     {ActionRow(StringSelect{CustomID: "s", Options: []SelectOption{{Label: "a", Value: "v"}, {Label: "b", Value: "v"}}})},
		"long option":          {ActionRow(StringSelect{CustomID: "s", Options: []SelectOption{{Label: long, Value: "v"}}})},
		"long placeholder":     {ActionRow(UserSelect{CustomID: "u", Placeholder: strings.Repeat("x", 151)})},
		"max values 26":        {ActionRow(RoleSelect{CustomID: "r", MaxValues: Ptr(26)})},
		"min above max":        {ActionRow(ChannelSelect{CustomID: "c", MinValues: Ptr(3), MaxValues: Ptr(2)})},
		"text input in row":    {ActionRow(TextInput{CustomID: "t", Label: "T"})},
		"mentionable select":   {ActionRow(Component{Type: 7, CustomID: "m"})},
	}
	for name, rows := range bad {
		if err := ValidateComponents(rows); !errors.Is(err, ErrInvalidComponent) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if err := ValidateComponents([]Component{ActionRow(six[:5]...)}); err != nil {
		t.Errorf("five buttons: %v", err)
	}
}

func TestModalLimitsAreEnforced(t *testing.T) {
	ok := NewModal("feedback", "Feedback", TextInput{CustomID: "subject", Label: "Subject", MaxLength: Ptr(80)},
		TextInput{CustomID: "body", Label: "What happened?", Style: TextInputParagraph, Required: Ptr(false), Placeholder: "Tell us more"})
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	in := func(id string) TextInput { return TextInput{CustomID: id, Label: "L"} }
	bad := map[string]Modal{
		"no rows":       NewModal("m", "T"),
		"six rows":      NewModal("m", "T", in("a"), in("b"), in("c"), in("d"), in("e"), in("f")),
		"long title":    NewModal("m", strings.Repeat("x", 46), in("a")),
		"no custom id":  NewModal("", "T", in("a")),
		"duplicate ids": NewModal("m", "T", in("a"), in("a")),
		"long label":    NewModal("m", "T", TextInput{CustomID: "a", Label: strings.Repeat("x", 46)}),
		"max length 0":  NewModal("m", "T", TextInput{CustomID: "a", Label: "L", MaxLength: Ptr(0)}),
		"min above max": NewModal("m", "T", TextInput{CustomID: "a", Label: "L", MinLength: Ptr(10), MaxLength: Ptr(5)}),
		"long value":    NewModal("m", "T", TextInput{CustomID: "a", Label: "L", Value: strings.Repeat("x", 4001)}),
		"button in row": {CustomID: "m", Title: "T", Components: []Component{ActionRow(PrimaryButton("a", "b"))}},
		"style 3":       NewModal("m", "T", TextInput{CustomID: "a", Label: "L", Style: 3}),
		"long holder":   NewModal("m", "T", TextInput{CustomID: "a", Label: "L", Placeholder: strings.Repeat("x", 101)}),
	}
	for name, m := range bad {
		if err := m.Validate(); !errors.Is(err, ErrInvalidComponent) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestAddBotLinkPutsTheManifestInTheFragment(t *testing.T) {
	m := Manifest{Name: "Mnemo", Description: "Answers questions.", Homepage: "https://example.org/mnemo",
		Intents: (IntentGuilds | IntentGuildMessages).Names(), Permissions: []string{"view_channel", "send_messages"},
		Commands: []CommandInput{{Name: "ask", Description: "Ask", Options: []CommandOption{{Name: "q", Description: "Q", Type: OptionString, Required: true}}}}}
	link, err := AddBotLink("https://talk.example.org/", m)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "https://talk.example.org/admin/bots/add#manifest="
	if !strings.HasPrefix(link, prefix) {
		t.Fatalf("link = %s", link)
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(link, prefix))
	if err != nil {
		t.Fatal(err)
	}
	var back Manifest
	if err := json.Unmarshal(raw, &back); err != nil || back.ManifestVersion != 1 || back.Name != "Mnemo" || back.Intents[1] != "GUILD_MESSAGES" || back.Commands[0].Options[0].Name != "q" {
		t.Fatalf("decoded = %+v, %v", back, err)
	}
}

func TestManifestRulesAreEnforced(t *testing.T) {
	bad := map[string]Manifest{
		"no name":        {},
		"long name":      {Name: strings.Repeat("x", 25)},
		"http homepage":  {Name: "a", Homepage: "http://example.org"},
		"gif avatar":     {Name: "a", Avatar: "data:image/gif;base64,AAAA"},
		"admin":          {Name: "a", Permissions: []string{"administrator"}},
		"unknown intent": {Name: "a", Intents: []string{"DIRECT_MESSAGES"}},
		"bad command":    {Name: "a", Commands: []CommandInput{{Name: "Ask"}}},
		"version 2":      {ManifestVersion: 2, Name: "a"},
	}
	for name, m := range bad {
		if _, err := AddBotLink("https://talk.example.org", m); !errors.Is(err, ErrInvalidManifest) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestVersionComesFromTheBuildInfo(t *testing.T) {
	dep := &debug.BuildInfo{Main: debug.Module{Path: "example.org/bot"}, Deps: []*debug.Module{{Path: modulePath, Version: "v0.0.0-20261113010203-abcdef123456"}}}
	if v := versionFrom(dep); v != "v0.0.0-20261113010203-abcdef123456" {
		t.Errorf("as a dependency: %s", v)
	}
	if v := versionFrom(&debug.BuildInfo{Main: debug.Module{Path: modulePath, Version: "(devel)"}}); v != "devel" {
		t.Errorf("as the main module in a work tree: %s", v)
	}
	if v := versionFrom(&debug.BuildInfo{Main: debug.Module{Path: "example.org/bot"}}); v != "devel" {
		t.Errorf("without the module: %s", v)
	}
	if !strings.HasPrefix(userAgent(), "mnema-talk-sdk-go/") {
		t.Errorf("user agent %s", userAgent())
	}
}
