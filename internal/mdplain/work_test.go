package mdplain

import (
	"strings"
	"testing"
)

// hostile are 4,000-unit messages built to make a scanner rescan: unclosed
// link openers (the review's worst case), brackets, parens, code fences,
// failing bare URLs, emphasis and spoiler openers.
var hostile = map[string]string{
	"link openers [a](":    strings.Repeat("[a](", 1000),
	"brackets [":           strings.Repeat("[", 4000),
	"label and paren [a](": strings.Repeat("[a](x ", 666),
	"parens (":             strings.Repeat("(", 4000),
	"mixed openers":        strings.Repeat("[a](*_~`<|", 400),
	"failing URLs":         strings.Repeat("-http://x:a/", 333),
	"doubtful URLs":        strings.Repeat("-http://x%41/*", 285),
	"backtick runs":        strings.Repeat("` `` ``` ", 444),
	"emphasis openers":     strings.Repeat("*a _a ~~a ||a ", 285),
	"spoiler pairs":        strings.Repeat("||a|| ", 666),
	"nested formats":       strings.Repeat("**_~~||", 571),
	"trailing parens":      strings.Repeat("-http://a.example/", 100) + strings.Repeat(")", 2200),
}

// Every message costs at most workPerUnit steps per unit plus workBase; one
// that would cost more fails closed instead of taking longer.
func TestPlainWorkIsLinearInTheMessage(t *testing.T) {
	for name, msg := range hostile {
		p, _ := parse(msg, "Spoiler")
		if p.steps > p.budget+len(msg) {
			t.Errorf("%s: %d steps for %d units, the bound is %d", name, p.steps, len(msg), p.budget)
		}
		t.Logf("%s: %d steps (%.1f per unit), doubt %v", name, p.steps, float64(p.steps)/float64(len(msg)), p.doubt)
	}
}

// The corpus of generated messages never comes near the bound: failing
// closed stays the exception for hosts only a browser can judge.
func TestOrdinaryMessagesStayFarBelowTheWorkBound(t *testing.T) {
	worst := 0.0
	readCross(t, "testdata/cross.jsonl", func(rec crossRecord) {
		p, _ := parse(strings.ReplaceAll(rec.Input, "\x00", ""), "Spoiler")
		if per := float64(p.steps) / float64(max(1, utf16Len(rec.Input))); per > worst {
			worst = per
		}
	})
	if worst > workPerUnit/4 {
		t.Errorf("a generated message took %.1f steps per unit (bound %d)", worst, workPerUnit)
	}
	t.Logf("worst %.1f steps per unit", worst)
}

func BenchmarkSnippetOfHostileMessages(b *testing.B) {
	for name, msg := range hostile {
		b.Run(name, func(b *testing.B) {
			for b.Loop() {
				_ = Snippet(msg, SnippetLen)
			}
		})
	}
}
