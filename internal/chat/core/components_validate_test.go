package core

import (
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func row(items ...Component) ActionRow { return ActionRow{Type: ComponentActionRow, Components: items} }

func button(id string) Component {
	return Component{Type: ComponentButton, Style: ButtonPrimary, Label: "Go", CustomID: id}
}

func stringSelect(id string, values ...string) Component {
	c := Component{Type: ComponentStringSelect, CustomID: id}
	for _, v := range values {
		c.Options = append(c.Options, SelectOption{Label: strings.ToUpper(v), Value: v})
	}
	return c
}

func TestValidComponentsAreAcceptedAndCleaned(t *testing.T) {
	rows := []ActionRow{
		row(
			Component{Type: ComponentButton, Style: ButtonSuccess, Label: "  Deploy ", CustomID: "deploy:v2.4", Placeholder: "dropped"},
			Component{Type: ComponentButton, Style: ButtonLink, Label: "Changelog", URL: "https://example.org/changes"},
			Component{Type: ComponentButton, Style: ButtonDanger, Emoji: &ComponentEmoji{Name: "👍"}, CustomID: "up"},
		),
		row(stringSelect("env", "staging", "prod")),
		row(Component{Type: ComponentUserSelect, CustomID: "who", MinValues: ptr(0), MaxValues: ptr(25)}),
		row(Component{Type: ComponentRoleSelect, CustomID: "roles"}),
		row(Component{Type: ComponentChannelSelect, CustomID: "where", Placeholder: "A channel"}),
	}
	got, err := ValidateComponents(rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 || got[0].Components[0].Label != "Deploy" || got[0].Components[0].Placeholder != "" {
		t.Fatalf("not cleaned: %+v", got[0])
	}
	if got[0].Components[1].URL != "https://example.org/changes" || got[0].Components[1].CustomID != "" {
		t.Fatalf("link button: %+v", got[0].Components[1])
	}
	if c, ok := FindComponent(got, "env"); !ok || len(c.Options) != 2 {
		t.Fatalf("FindComponent: %+v %v", c, ok)
	}
	if _, ok := FindComponent(got, ""); ok {
		t.Fatal("a link button was found by an empty custom_id")
	}
}

func TestNoComponentsIsNil(t *testing.T) {
	if got, err := ValidateComponents(nil); err != nil || got != nil {
		t.Fatalf("%v %v", got, err)
	}
	if got, err := ValidateComponents([]ActionRow{}); err != nil || got != nil {
		t.Fatalf("%v %v", got, err)
	}
}

func TestComponentsThatBreakTheRulesAreRefused(t *testing.T) {
	long := strings.Repeat("x", 101)
	six := []Component{button("a"), button("b"), button("c"), button("d"), button("e"), button("f")}
	cases := map[string][]ActionRow{
		"six rows":                 {row(button("1")), row(button("2")), row(button("3")), row(button("4")), row(button("5")), row(button("6"))},
		"a button at the top":      {{Type: ComponentButton, Components: []Component{button("a")}}},
		"a mentionable select":     {row(Component{Type: 7, CustomID: "m"})},
		"an unknown type":          {row(Component{Type: 17, CustomID: "m"})},
		"a nested row":             {row(Component{Type: ComponentActionRow, CustomID: "a"})},
		"a text input":             {row(Component{Type: ComponentTextInput, CustomID: "t", Label: "T", Style: 1})},
		"an empty row":             {row()},
		"six buttons in a row":     {row(six...)},
		"a select with a button":   {row(stringSelect("s", "a"), button("b"))},
		"a duplicate custom_id":    {row(button("same")), row(button("same"))},
		"a missing custom_id":      {row(button(""))},
		"a too long custom_id":     {row(button(long))},
		"a control in custom_id":   {row(button("a\x01b"))},
		"a bad style":              {row(Component{Type: ComponentButton, Style: 6, Label: "x", CustomID: "a"})},
		"a link with custom_id":    {row(Component{Type: ComponentButton, Style: ButtonLink, Label: "x", URL: "https://a.example", CustomID: "a"})},
		"a link without url":       {row(Component{Type: ComponentButton, Style: ButtonLink, Label: "x"})},
		"a javascript link":        {row(Component{Type: ComponentButton, Style: ButtonLink, Label: "x", URL: "javascript:alert(1)"})},
		"a too long link":          {row(Component{Type: ComponentButton, Style: ButtonLink, Label: "x", URL: "https://a.example/" + strings.Repeat("a", 512)})},
		"a url on a plain button":  {row(Component{Type: ComponentButton, Style: 1, Label: "x", CustomID: "a", URL: "https://a.example"})},
		"no label nor emoji":       {row(Component{Type: ComponentButton, Style: 1, CustomID: "a"})},
		"a too long label":         {row(Component{Type: ComponentButton, Style: 1, CustomID: "a", Label: strings.Repeat("x", 81)})},
		"a custom emoji":           {row(Component{Type: ComponentButton, Style: 1, CustomID: "a", Emoji: &ComponentEmoji{Name: "<:party:123e4567-e89b-12d3-a456-426614174000>"}})},
		"a word as emoji":          {row(Component{Type: ComponentButton, Style: 1, CustomID: "a", Emoji: &ComponentEmoji{Name: ":custom:"}})},
		"a select without options": {row(Component{Type: ComponentStringSelect, CustomID: "s"})},
		"26 options":               {row(stringSelect("s", strings.Split(strings.Repeat("v,", 26), ",")[:26]...))},
		"duplicate option values":  {row(stringSelect("s", "a", "a"))},
		"an option without label":  {row(Component{Type: ComponentStringSelect, CustomID: "s", Options: []SelectOption{{Value: "a"}}})},
		"a too long option value":  {row(stringSelect("s", long))},
		"max above the options":    {row(func() Component { c := stringSelect("s", "a", "b"); c.MaxValues = ptr(3); return c }())},
		"min above max":            {row(Component{Type: ComponentUserSelect, CustomID: "u", MinValues: ptr(3), MaxValues: ptr(2)})},
		"max 0":                    {row(Component{Type: ComponentUserSelect, CustomID: "u", MaxValues: ptr(0)})},
		"min 26":                   {row(Component{Type: ComponentRoleSelect, CustomID: "u", MinValues: ptr(26)})},
		"options on a user select": {row(Component{Type: ComponentUserSelect, CustomID: "u", Options: []SelectOption{{Label: "a", Value: "a"}}})},
		"a too long placeholder":   {row(Component{Type: ComponentChannelSelect, CustomID: "c", Placeholder: strings.Repeat("p", 151)})},
		"more defaults than max": {row(Component{Type: ComponentStringSelect, CustomID: "s", Options: []SelectOption{
			{Label: "A", Value: "a", Default: true}, {Label: "B", Value: "b", Default: true}}})},
	}
	for name, rows := range cases {
		if _, err := ValidateComponents(rows); err == nil {
			t.Errorf("%s: accepted", name)
		} else if strings.Contains(err.Error(), "empty") {
			t.Errorf("%s: %q would read as Discord's empty message (50006)", name, err)
		}
	}
}

func TestATypeMnemaTalkDoesNotOfferIsNamed(t *testing.T) {
	_, err := ValidateComponents([]ActionRow{row(Component{Type: 7, CustomID: "m"})})
	if err == nil || !strings.Contains(err.Error(), "type 7 is not supported") {
		t.Fatalf("err = %v", err)
	}
}

func input(id string) Component {
	return Component{Type: ComponentTextInput, CustomID: id, Label: "Subject", Style: TextInputShort}
}

func TestAValidModalIsAccepted(t *testing.T) {
	m := Modal{CustomID: "feedback", Title: " Feedback ", Components: []ActionRow{
		row(Component{Type: ComponentTextInput, CustomID: "subject", Label: "Subject", Style: 1, MaxLength: ptr(80), Value: "Login"}),
		row(Component{Type: ComponentTextInput, CustomID: "body", Label: "What happened?", Style: 2, Required: ptr(false), Placeholder: "Tell us more"}),
	}}
	got, err := ValidateModal(m)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Feedback" || len(got.Components) != 2 || got.Components[0].Components[0].Value != "Login" {
		t.Fatalf("%+v", got)
	}
}

func TestModalsThatBreakTheRulesAreRefused(t *testing.T) {
	ok := []ActionRow{row(input("a"))}
	cases := map[string]Modal{
		"no custom_id":           {Title: "T", Components: ok},
		"no title":               {CustomID: "m", Components: ok},
		"a too long title":       {CustomID: "m", Title: strings.Repeat("t", 46), Components: ok},
		"no rows":                {CustomID: "m", Title: "T"},
		"six rows":               {CustomID: "m", Title: "T", Components: []ActionRow{row(input("1")), row(input("2")), row(input("3")), row(input("4")), row(input("5")), row(input("6"))}},
		"two inputs in a row":    {CustomID: "m", Title: "T", Components: []ActionRow{row(input("a"), input("b"))}},
		"a button":               {CustomID: "m", Title: "T", Components: []ActionRow{row(button("b"))}},
		"an input at the top":    {CustomID: "m", Title: "T", Components: []ActionRow{{Type: ComponentTextInput}}},
		"a duplicate input":      {CustomID: "m", Title: "T", Components: []ActionRow{row(input("a")), row(input("a"))}},
		"no label":               {CustomID: "m", Title: "T", Components: []ActionRow{row(Component{Type: 4, CustomID: "a", Style: 1})}},
		"a too long label":       {CustomID: "m", Title: "T", Components: []ActionRow{row(Component{Type: 4, CustomID: "a", Style: 1, Label: strings.Repeat("l", 46)})}},
		"style 3":                {CustomID: "m", Title: "T", Components: []ActionRow{row(Component{Type: 4, CustomID: "a", Style: 3, Label: "L"})}},
		"min_length 4001":        {CustomID: "m", Title: "T", Components: []ActionRow{row(Component{Type: 4, CustomID: "a", Style: 1, Label: "L", MinLength: ptr(4001)})}},
		"max_length 0":           {CustomID: "m", Title: "T", Components: []ActionRow{row(Component{Type: 4, CustomID: "a", Style: 1, Label: "L", MaxLength: ptr(0)})}},
		"min above max":          {CustomID: "m", Title: "T", Components: []ActionRow{row(Component{Type: 4, CustomID: "a", Style: 1, Label: "L", MinLength: ptr(10), MaxLength: ptr(5)})}},
		"a value above max":      {CustomID: "m", Title: "T", Components: []ActionRow{row(Component{Type: 4, CustomID: "a", Style: 1, Label: "L", MaxLength: ptr(2), Value: "abc"})}},
		"a line break in short":  {CustomID: "m", Title: "T", Components: []ActionRow{row(Component{Type: 4, CustomID: "a", Style: 1, Label: "L", Value: "a\nb"})}},
		"a too long placeholder": {CustomID: "m", Title: "T", Components: []ActionRow{row(Component{Type: 4, CustomID: "a", Style: 1, Label: "L", Placeholder: strings.Repeat("p", 101)})}},
	}
	for name, m := range cases {
		if _, err := ValidateModal(m); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestAnAnswerMustFitItsInput(t *testing.T) {
	short := Component{Type: 4, CustomID: "s", Style: TextInputShort, MinLength: ptr(2), MaxLength: ptr(5)}
	optional := Component{Type: 4, CustomID: "o", Style: TextInputParagraph, Required: ptr(false)}
	if got, err := CheckInputAnswer(short, "  abc "); err != nil || got != "abc" {
		t.Fatalf("%q %v", got, err)
	}
	for _, bad := range []string{"", "a", "abcdef", "a\nb"} {
		if _, err := CheckInputAnswer(short, bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if got, err := CheckInputAnswer(optional, ""); err != nil || got != "" {
		t.Fatalf("an optional input left out: %q %v", got, err)
	}
	if got, err := CheckInputAnswer(optional, "line one\nline two"); err != nil || got != "line one\nline two" {
		t.Fatalf("a paragraph keeps its lines: %q %v", got, err)
	}
	if _, err := CheckInputAnswer(optional, strings.Repeat("ä", MaxInputLen+1)); err == nil {
		t.Fatal("an answer over 4000 characters accepted")
	}
}
