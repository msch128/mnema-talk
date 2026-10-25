package automod

import (
	"strings"
	"testing"
)

// FuzzHostOf checks the host reader never panics and only returns hosts in the
// form links are compared in: lower case, no trailing dot, nothing that could
// be read as another part of a URL.
func FuzzHostOf(f *testing.F) {
	for _, s := range []string{
		"https://example.com/path", "http://allowed.com@evil.com", "evil.com\\@allowed.com", "https://EXAMPLE.com.", "https://example.com:8080/x",
		"https://xn--bcher-kva.example", "https://b\u00fccher.example", "https://example。com", "https://%65xample.com", "https://[::1]:80/", "https://",
		"www.example.com", "https://a..b", "https://exa mple.com", "\x00", "https://ｅxample.com", "",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, link string) {
		host, ok := hostOf(link)
		if !ok {
			return
		}
		if host == "" || host != strings.ToLower(host) {
			t.Fatalf("hostOf(%q) = %q", link, host)
		}
		if !strings.HasPrefix(host, "[") && (strings.HasSuffix(host, ".") || strings.ContainsAny(host, "/?#\\@ ")) {
			t.Fatalf("hostOf(%q) = %q carries URL syntax", link, host)
		}
	})
}

// FuzzCleanDomain checks that a domain accepted for a rule list is already in
// the form hostOf reads, so a listed domain and a link to it always compare equal.
func FuzzCleanDomain(f *testing.F) {
	for _, s := range []string{"example.com", "*.example.com", "https://Example.COM/x", "b\u00fccher.example", "xn--bcher-kva.example", "a", "", "-a.com", "a.b"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		d, err := cleanDomain(s)
		if err != nil || d == "" {
			return
		}
		if host, ok := hostOf(d); !ok || host != d {
			t.Fatalf("cleanDomain(%q) = %q but hostOf gives %q", s, d, host)
		}
	})
}

// FuzzLinkRules checks the rules fail closed: any link-like token breaks a
// block-all rule and an allow-list that lists nothing, however the rest of the
// message looks, and a blocked host stays blocked when text follows it after
// a space of any kind.
func FuzzLinkRules(f *testing.F) {
	for _, s := range []string{"see https://evil.example now", "https://evil.example now", "https://evil.example　x", "WWW.evil.example", "http://évil.example/x", "​https://evil.example", "no link here"} {
		f.Add(s)
	}
	blockAll := &compiled{Rule: Rule{Config: Settings{Mode: LinksBlockAll}}}
	allowNone := &compiled{Rule: Rule{Config: Settings{Mode: LinksAllowList}}}
	blockList := &compiled{Rule: Rule{Config: Settings{Mode: LinksBlockList, Domains: []string{"evil.example"}}}, domains: []string{"evil.example"}}
	f.Fuzz(func(t *testing.T, content string) {
		linky := len(links(content)) > 0
		if linky && (!blockAll.matchesLinks(content) || !allowNone.matchesLinks(content)) {
			t.Fatalf("a link in %q passed a strict rule", content)
		}
		if !linky && (blockAll.matchesLinks(content) || allowNone.matchesLinks(content)) {
			t.Fatalf("%q has no link but broke a rule", content)
		}
		for _, space := range []string{" ", " ", " ", "　", " ", "\t"} {
			msg := "https://evil.example" + space + content
			if !blockList.matchesLinks(msg) {
				t.Fatalf("blocked host escaped with %q after it: %q", space, content)
			}
		}
	})
}

// FuzzMatchWords checks that compiling a word list never panics, that a clean
// word is found in a message that holds it between spaces, and that matching
// arbitrary content never panics.
func FuzzMatchWords(f *testing.F) {
	for _, s := range [][2]string{{"spam", "this is SPAM"}, {"buy now", "buy   now"}, {"cheat*", "cheating"}, {"a.b", "a.b"}, {"(x)", "(x)"}, {"café", "CAFÉ"}, {"\\", "\\"}, {"[", "["}} {
		f.Add(s[0], s[1])
	}
	f.Fuzz(func(t *testing.T, word, content string) {
		w, err := cleanWord(word)
		if err != nil || w == "" || strings.Contains(w, "*") {
			return
		}
		c, err := compile(Rule{Kind: KindWords, Config: Settings{Words: []string{w}}})
		if err != nil {
			return
		}
		_ = c.matchesWords(content)
		if fold(w) == w && c.words != nil && !c.matchesWords(" "+w+" ") {
			t.Fatalf("word %q not found between spaces", w)
		}
	})
}
