package poll

import (
	"strings"
	"testing"
	"time"
)

func valid() Input {
	return Input{Question: "  Pizza or pasta?  ", Options: []string{" Pizza ", "Pasta"}}
}

func TestInputIsTrimmedAndDefaultsToADay(t *testing.T) {
	c, err := valid().clean()
	if err != nil {
		t.Fatal(err)
	}
	if c.Question != "Pizza or pasta?" || c.Options[0] != "Pizza" || c.Duration != "24h" || c.length != 24*time.Hour {
		t.Fatalf("clean: %+v", c)
	}
}

func TestEveryOfferedDurationHasALength(t *testing.T) {
	want := map[string]time.Duration{"1h": time.Hour, "4h": 4 * time.Hour, "8h": 8 * time.Hour, "24h": 24 * time.Hour, "3d": 72 * time.Hour, "1w": 168 * time.Hour}
	for _, name := range DurationNames() {
		in := valid()
		in.Duration = name
		c, err := in.clean()
		if err != nil || c.length != want[name] {
			t.Errorf("%s: %v, %v", name, c.length, err)
		}
	}
	if len(DurationNames()) != len(want) {
		t.Fatalf("durations: %v", DurationNames())
	}
}

func TestInputBoundsAreEnforced(t *testing.T) {
	tooMany := make([]string, MaxOptions+1)
	for i := range tooMany {
		tooMany[i] = strings.Repeat("x", i+1)
	}
	cases := map[string]func(*Input){
		"empty question":      func(in *Input) { in.Question = "  " },
		"long question":       func(in *Input) { in.Question = strings.Repeat("é", MaxQuestion+1) },
		"one option":          func(in *Input) { in.Options = []string{"a"} },
		"eleven options":      func(in *Input) { in.Options = tooMany },
		"empty option":        func(in *Input) { in.Options = []string{"a", " "} },
		"long option":         func(in *Input) { in.Options = []string{"a", strings.Repeat("b", MaxLabel+1)} },
		"multi-line option":   func(in *Input) { in.Options = []string{"a", "b\nc"} },
		"duplicate option":    func(in *Input) { in.Options = []string{"Yes", "yes"} },
		"unknown duration":    func(in *Input) { in.Duration = "5m" },
		"NUL in the question": func(in *Input) { in.Question = "a\x00b" },
	}
	for name, mutate := range cases {
		in := valid()
		mutate(&in)
		if _, err := in.clean(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	exact := valid()
	exact.Question = strings.Repeat("é", MaxQuestion)
	exact.Options = []string{strings.Repeat("\u00fc", MaxLabel), "b"}
	if _, err := exact.clean(); err != nil {
		t.Fatalf("values at the limit: %v", err)
	}
}

func TestSplitAnswersDropsEmptyPieces(t *testing.T) {
	got := splitAnswers(" Yes |No||  | Maybe | ")
	if strings.Join(got, ",") != "Yes,No,Maybe" {
		t.Fatalf("%q", got)
	}
	if len(splitAnswers("")) != 0 {
		t.Fatal("empty input gives answers")
	}
}

func TestLookAlikeOptionsAreDuplicates(t *testing.T) {
	for name, pair := range map[string][2]string{
		"case":                {"Yes", "YES"},
		"composed/decomposed": {"Café", "Café"},
		"invisible joiner":    {"Pizza", "Piz​za"},
		"fullwidth":           {"Pizza", "Ｐizza"},
	} {
		in := valid()
		in.Options = []string{pair[0], pair[1]}
		if _, err := in.clean(); err == nil {
			t.Errorf("%s: %q and %q were both accepted", name, pair[0], pair[1])
		}
	}
}
