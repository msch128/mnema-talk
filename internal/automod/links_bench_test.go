package automod

import (
	"strings"
	"testing"
)

// The link rules run on every message a member posts. These are the shapes a
// community writes most: no link at all, one link, Markdown around links, a
// message in another script, and a long message at the size limit.
var benchMessages = map[string]string{
	"no link":   "hey, are we still on for tonight? I will bring the snacks and the board game",
	"one link":  "the docs are at https://docs.allowed.example/guide/start?ref=chat#install, have a look",
	"markdown":  "**read this:** [the guide](https://allowed.example/guide) and ||https://other.example/x|| or <https://allowed.example/a>",
	"non-latin": "Привет! Смотри https://пример.example/страница и напиши мне потом, ладно?",
	"long":      strings.Repeat("words and more words, then https://allowed.example/p/1 and www.other.example. ", 50),
	// Hostile messages at the length limit (4000 characters).
	"closers": "https://" + strings.Repeat("a*", 1990) + "x.example",
	"schemes": strings.Repeat("http://a ", 444),
	"nested":  strings.Repeat("https://a.example/", 222),
	"www":     strings.Repeat("www.", 1000),
	"slashes": strings.Repeat("https:///", 444),
	"parens":  strings.Repeat("https://a.example/)", 210),
}

// worstLinkMessages are the most expensive shapes the review of the fix found
// (fixreview-H-L3-01-04, S1), each at the length limit of 4000 characters.
var worstLinkMessages = map[string]string{
	// One authority of non-ASCII letters with 15 closers at its end: every
	// closer is a place to end the host, and each reading runs IDNA.
	"idna closers": "https://" + strings.Repeat("é", 3962) + strings.Repeat("*é", 15),
	// Nested schemes over one shared tail of trailing punctuation.
	"nested tail": strings.Repeat("https://a", 280) + strings.Repeat(".", 1480),
	// 40 nested links, each a non-ASCII host of 40 letters with 15 closers.
	"nested idna": strings.Repeat("https://"+strings.Repeat("é", 25)+strings.Repeat("*é", 15)+"/", 40),
	// One token of runs of trailing punctuation and Markdown markers.
	"trailing runs": "https://a" + strings.Repeat(".,;:!?*_~|", 199),
}

func BenchmarkMatchesLinks(b *testing.B) {
	rules := map[string]*compiled{
		"allow": linksRule(LinksAllowList, "allowed.example"),
		"block": linksRule(LinksBlockList, "other.example"),
	}
	for rule, r := range rules {
		for name, msg := range benchMessages {
			b.Run(rule+"/"+name, func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					r.matchesLinks(msg)
				}
			})
		}
	}
}

func BenchmarkLinksWorstCase(b *testing.B) {
	for name, msg := range worstLinkMessages {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				links(msg)
			}
		})
	}
}
