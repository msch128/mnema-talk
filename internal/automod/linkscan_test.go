package automod

import (
	"strings"
	"testing"
)

func TestHostsAreReadByTheURLStandardNotByGo(t *testing.T) {
	for _, c := range []struct {
		authority, host string
		exact, rejected bool
	}{
		{"Other.Example", "other.example", true, false},
		{"user:pw@other.example:443", "other.example", true, false},
		{"0x7f.1", "127.0.0.1", true, false},     // IPv4 shorthand
		{"2130706433", "127.0.0.1", true, false}, // one number
		{"0177.0.0.1", "127.0.0.1", true, false}, // octal
		{"1.2.3.256", "1.2.3.256", false, true},  // not an address, and a number at the end
		{"other%2eexample", "other.example", true, false},
		{"exa%zzmple.com", "exa%zzmple.com", false, true},
		{"_.other.example", "_.other.example", true, false}, // no STD3 rules in a browser
		{"a-.other.example", "a-.other.example", true, false},
		{"allowed\u0301.example", "xn--allowed-zge.example", true, false}, // the mark stays
		{"other\uff0eexample", "other.example", true, false},
		{"other.example:99999", "other.example", false, true},
		{"other.example:8o", "other.example", false, true},
		{"[0:0::1]:80", "[::1]", true, false},
		{"[::1", "[::1", false, true},
		{"[1.2.3.4]", "[1.2.3.4]", false, true},
		{"other.example|x", "other.example|x", false, true},
		{"", "", false, true},
	} {
		r := readAuthority(c.authority)
		if r.host != c.host || r.exact != c.exact || r.rejected != c.rejected {
			t.Errorf("readAuthority(%q) = %+v, want host %q exact %v rejected %v", c.authority, r, c.host, c.exact, c.rejected)
		}
	}
}

func TestEveryPlaceTheClientCanEndAHostIsJudged(t *testing.T) {
	block := linksRule(LinksBlockList, "other.example")
	allow := linksRule(LinksAllowList, "allowed.example")
	for _, msg := range []string{
		"**https://other.example**.allowed.example", "__https://other.example__.allowed.example",
		"||https://other.example||.allowed.example", "~~https://other.example~~.allowed.example",
		"[docs](https://other.example).allowed.example", "*https://other.example*.allowed.example",
	} {
		if !block.matchesLinks(msg) || !allow.matchesLinks(msg) {
			t.Errorf("%q: the client links other.example, block list %v, allow list %v", msg, block.matchesLinks(msg), allow.matchesLinks(msg))
		}
	}
	for _, msg := range []string{"**https://allowed.example**", "https://allowed.example's page", "[https://allowed.example](https://allowed.example/x)."} {
		if allow.matchesLinks(msg) {
			t.Errorf("%q: the client links allowed.example only, but the allow list refused it", msg)
		}
	}
}

func TestATangledAuthorityMayLeadAnywhere(t *testing.T) {
	msg := "https://" + strings.Repeat("a*", 2000) + "other.example"
	got := links(msg)
	if len(got) == 0 || !got[len(got)-1].opaque {
		t.Fatalf("links of a tangled authority = %d, the last not opaque", len(got))
	}
	if !linksRule(LinksBlockList, "unrelated.example").matchesLinks(msg) || !linksRule(LinksAllowList, "other.example").matchesLinks(msg) {
		t.Error("a link that may lead anywhere passed a list")
	}
}

func TestTextThatOnlyFoldingTurnsIntoALinkStillCounts(t *testing.T) {
	for _, msg := range []string{"\uff48\uff54\uff54\uff50\uff53://other.example", "ht\u200btps://other.example"} {
		if got := links(msg); len(got) != 1 || got[0].ok {
			t.Errorf("links(%q) = %+v, want one link nobody can read", msg, got)
		}
	}
}
