package automod

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func wordsRule(t *testing.T, words ...string) *compiled {
	t.Helper()
	c, err := compile(Rule{Kind: KindWords, Config: Settings{Words: words}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestBlockedWordsMatchWholeWordsWithoutRegardToCase(t *testing.T) {
	r := wordsRule(t, "spam", "buy now", "cheat*")
	for content, want := range map[string]bool{
		"this is SPAM!":            true,
		"spam":                     true,
		"(spam)":                   true,
		"spammer":                  false, // part of a longer word
		"antispam":                 false,
		"Buy   Now, friends":       true, // a phrase: any white space between its words
		"buy later":                false,
		"cheating is bad":          true, // wildcard: cheat + letters
		"cheat":                    true,
		"a cheater":                true,
		"sp​am":                    true, // invisible characters do not help
		"nothing to see":           false,
		"":                         false,
		"cheat-code":               true,
		"a_spam":                   false, // underscore belongs to a word
		"Spam.":                    true,
		"café spam café":           true,
		"stop the \"spam\" please": true,
	} {
		if got := r.matchesWords(content); got != want {
			t.Errorf("%q: got %v, want %v", content, got, want)
		}
	}
}

func TestAnEmptyWordListMatchesNothing(t *testing.T) {
	if wordsRule(t).matchesWords("anything at all") {
		t.Fatal("an empty list blocks nothing")
	}
}

func TestSpecialCharactersInWordsAreLiteral(t *testing.T) {
	r := wordsRule(t, "a.b", "c++", "(x)")
	if !r.matchesWords("say a.b now") || r.matchesWords("say aXb now") {
		t.Error("a dot is a dot")
	}
	if !r.matchesWords("learn c++ today") {
		t.Error("c++")
	}
}

func linksRule(mode string, domains ...string) *compiled {
	c, _ := compile(Rule{Kind: KindLinks, Config: Settings{Mode: mode, Domains: domains}})
	return c
}

func TestLinkFilterModes(t *testing.T) {
	const page = "look at https://sub.Example.com/a?b=1 and www.other.org."
	cases := []struct {
		name string
		rule *compiled
		text string
		want bool
	}{
		{"block all, a link", linksRule(LinksBlockAll), "see http://x.io", true},
		{"block all, none", linksRule(LinksBlockAll), "no links, only example.com text", false},
		{"allow list, all allowed", linksRule(LinksAllowList, "example.com", "other.org"), page, false},
		{"allow list, one not allowed", linksRule(LinksAllowList, "example.com"), page, true},
		{"allow list covers subdomains only", linksRule(LinksAllowList, "ample.com"), "https://example.com", true},
		{"block list hit", linksRule(LinksBlockList, "other.org"), page, true},
		{"block list miss", linksRule(LinksBlockList, "evil.net"), page, false},
		{"block list ignores look-alikes", linksRule(LinksBlockList, "evil.net"), "https://notevil.net", false},
		{"credentials in a link do not hide the host", linksRule(LinksBlockList, "evil.net"), "https://good.com@evil.net/x", true},
		{"port is not part of the host", linksRule(LinksBlockList, "evil.net"), "http://evil.net:8080/x", true},
		{"punctuation after a link", linksRule(LinksBlockList, "evil.net"), "(see https://evil.net/x).", true},
	}
	for _, c := range cases {
		if got := c.rule.matchesLinks(c.text); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestMentionsAreCountedOncePerName(t *testing.T) {
	for content, want := range map[string]int{
		"hi @alice @bobby":                 2,
		"@alice @ALICE @alice.":            1,
		"@all and @here":                   2,
		"mail me at a@example.com":         0, // not a mention: a character precedes the @
		"@ab is too short, @abc is not":    1,
		"":                                 0,
		"(@alice) and,@bobby and @carol_1": 3,
	} {
		if got := distinctMentions(content); got != want {
			t.Errorf("%q: got %d, want %d", content, got, want)
		}
	}
}

func TestExemptionsByChannelAndRole(t *testing.T) {
	ch, role, other := uuid.New(), uuid.New(), uuid.New()
	r := &compiled{exemptChannels: set([]uuid.UUID{ch}), exemptRoles: set([]uuid.UUID{role})}
	if !exempt(r, ch, nil) || !exempt(r, other, []uuid.UUID{other, role}) || exempt(r, other, []uuid.UUID{other}) {
		t.Fatal("exemption by channel or role")
	}
}

func TestRuleInputIsValidatedPerKind(t *testing.T) {
	base := RuleInput{Name: "  A rule ", Enabled: true, Action: ActionBlock}
	with := func(kind string, c Settings) RuleInput { in := base; in.Kind, in.Config = kind, c; return in }

	good, err := with(KindWords, Settings{Words: []string{" Spam ", "spam", "", "buy   now"}, Mode: "ignored"}).Normalize()
	if err != nil || good.Name != "A rule" || len(good.Config.Words) != 2 || good.Config.Words[1] != "buy now" || good.Config.Mode != "" {
		t.Fatalf("words: %+v %v", good, err)
	}
	dom, err := with(KindLinks, Settings{Mode: LinksAllowList, Domains: []string{"https://Docs.Example.com/path", "*.example.org", "example.org"}}).Normalize()
	if err != nil || len(dom.Config.Domains) != 2 || dom.Config.Domains[0] != "docs.example.com" || dom.Config.Domains[1] != "example.org" {
		t.Fatalf("links: %+v %v", dom, err)
	}
	raid, err := with(KindRaid, Settings{Joins: 10, WindowSeconds: 10, RestrictSeconds: 600}).Normalize()
	if err != nil || raid.Action != ActionFlag || raid.TimeoutSeconds != 0 {
		t.Fatalf("raid only flags: %+v %v", raid, err)
	}

	bad := map[string]RuleInput{
		"unknown kind":          with("other", Settings{}),
		"words with only stars": with(KindWords, Settings{Words: []string{"***"}}),
		"too many words":        with(KindWords, Settings{Words: make([]string, MaxWords+1)}),
		"word too long":         with(KindWords, Settings{Words: []string{strings.Repeat("a", MaxWordLen+1)}}),
		"links without mode":    with(KindLinks, Settings{}),
		"not a domain":          with(KindLinks, Settings{Mode: LinksBlockList, Domains: []string{"localhost"}}),
		"mentions zero":         with(KindMentions, Settings{}),
		"mentions too many":     with(KindMentions, Settings{MaxMentions: MaxMentionsLimit + 1}),
		"account age too short": with(KindNewAccount, Settings{MaxAgeSeconds: 5, Restrict: RestrictLinks}),
		"account restrict":      with(KindNewAccount, Settings{MaxAgeSeconds: 600, Restrict: "x"}),
		"raid joins":            with(KindRaid, Settings{Joins: 1, WindowSeconds: 10, RestrictSeconds: 600}),
		"raid window":           with(KindRaid, Settings{Joins: 5, WindowSeconds: 1, RestrictSeconds: 600}),
		"raid restriction":      with(KindRaid, Settings{Joins: 5, WindowSeconds: 10, RestrictSeconds: 1}),
		"empty name":            {Kind: KindMentions, Name: " ", Action: ActionBlock, Config: Settings{MaxMentions: 5}},
		"unknown action":        {Kind: KindMentions, Name: "x", Action: "ban", Config: Settings{MaxMentions: 5}},
		"timeout too short":     {Kind: KindMentions, Name: "x", Action: ActionBlock, TimeoutSeconds: 1, Config: Settings{MaxMentions: 5}},
		"too many exempt roles": {Kind: KindMentions, Name: "x", Action: ActionBlock, Config: Settings{MaxMentions: 5}, ExemptRoles: manyIDs(MaxExemptions + 1)},
	}
	for name, in := range bad {
		if _, err := in.Normalize(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func manyIDs(n int) []uuid.UUID {
	ids := make([]uuid.UUID, n)
	for i := range ids {
		ids[i] = uuid.New()
	}
	return ids
}

func TestLinkHostsAreReadTheWayBrowsersDo(t *testing.T) {
	allow := linksRule(LinksAllowList, "allowed.com", "xn--bcher-kva.example")
	block := linksRule(LinksBlockList, "evil.com")
	for _, c := range []struct {
		name, text string
		allowHit   bool // the allow list forbids it
		blockHit   bool // the block list forbids it
	}{
		{"plain allowed link", "http://allowed.com/x", false, false},
		{"full-width dots", "http://evil．com/x", true, true},
		{"ideographic dot", "http://evil。com/x", true, true},
		{"percent-encoded host", "http://%65vil.com/x", true, true},
		{"homograph of an allowed domain", "http://аllowed.com/x", true, false},
		{"userinfo trick", "http://allowed.com@evil.com/x", true, true},
		{"userinfo trick the other way", "http://evil.com@allowed.com/x", false, false},
		{"unicode form of a listed domain", "http://bücher.example/x", false, false},
		{"upper case and trailing dot", "HTTP://EVIL.COM./x", true, true},
		{"backslash ends the host", "http://evil.com\\allowed.com", true, true},
		{"unparseable token", "http://exa%zzmple.com/x", true, false},
		{"empty host", "http:///x", true, false},
	} {
		if got := allow.matchesLinks(c.text); got != c.allowHit {
			t.Errorf("%s: allow list %v, want %v", c.name, got, c.allowHit)
		}
		if got := block.matchesLinks(c.text); got != c.blockHit {
			t.Errorf("%s: block list %v, want %v", c.name, got, c.blockHit)
		}
	}
	if !linksRule(LinksBlockAll).matchesLinks("http://exa%zzmple.com/x") {
		t.Error("an unreadable link is a link for block-all")
	}
	if got := links("see http://exa%zzmple.com/x"); len(got) != 1 || got[0].ok {
		t.Errorf("an unreadable link still counts for the new-account rule: %+v", got)
	}
}

func TestListedDomainsAreStoredInPunycode(t *testing.T) {
	in := RuleInput{Kind: KindLinks, Name: "l", Action: ActionBlock, Config: Settings{Mode: LinksBlockList,
		Domains: []string{"Bücher.Example", "http://evil．com/x", "xn--bcher-kva.example"}}}
	got, err := in.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"xn--bcher-kva.example", "evil.com"}
	if len(got.Config.Domains) != 2 || got.Config.Domains[0] != want[0] || got.Config.Domains[1] != want[1] {
		t.Fatalf("domains: %v", got.Config.Domains)
	}
}
