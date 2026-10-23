package bot

import (
	"strings"
	"testing"
)

func choiceCommand(typ int, choices ...string) CommandInput {
	return CommandInput{Name: "c", Description: "d", Options: []Option{{Name: "o", Description: "d", Type: typ, Choices: choices}}}
}

func TestOptionChoicesAreValidatedWhenRegistered(t *testing.T) {
	for name, in := range map[string]CommandInput{
		"on an integer": choiceCommand(OptionInteger, "1"),
		"duplicates":    choiceCommand(OptionString, "a", "a"),
		"empty choice":  choiceCommand(OptionString, "a", " "),
		"long choice":   choiceCommand(OptionString, strings.Repeat("x", 101)),
		"too many":      choiceCommand(OptionString, make([]string, MaxChoices+1)...),
	} {
		if _, err := in.Clean(); err == nil {
			t.Errorf("choices %s were accepted", name)
		}
	}
	cleaned, err := choiceCommand(OptionString, " small ", "large").Clean()
	if err != nil || cleaned.Options[0].Choices[0] != "small" {
		t.Fatalf("clean: %+v, %v", cleaned, err)
	}
	if plain, _ := choiceCommand(OptionString).Clean(); plain.Options[0].Choices != nil {
		t.Fatalf("an empty list stays empty: %+v", plain)
	}
}

func TestAnOptionWithChoicesTakesOnlyThoseValues(t *testing.T) {
	cleaned, err := choiceCommand(OptionString, "small", "large").Clean()
	if err != nil {
		t.Fatal(err)
	}
	o := cleaned.Options[0]
	if v, err := checkChoice(o, " large "); err != nil || v != "large" {
		t.Fatalf("a choice: %v, %v", v, err)
	}
	if _, err := checkChoice(o, "medium"); err == nil {
		t.Fatal("a value outside the choices was accepted")
	}
	free := Option{Name: "free", Type: OptionString}
	if v, err := checkChoice(free, "anything"); err != nil || v != "anything" {
		t.Fatalf("free text: %v, %v", v, err)
	}
}
