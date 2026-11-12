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
