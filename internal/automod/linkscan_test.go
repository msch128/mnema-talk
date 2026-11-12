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

// The client reads at most maxDestination UTF-16 code units of the target of
// [text](url) and cuts there, so a longer authority may open any prefix of
// itself: no list lets it through, masked or bare.
func TestAnAuthorityLongerThanTheClientsTargetCapLeadsAnywhere(t *testing.T) {
	long := "https://" + strings.Repeat("a", maxDestination) + ".allowed.example/x"
	for _, msg := range []string{long, "[text](" + long + ")", "[text](<" + long + ">)", "[text](" + long + " 'title')"} {
		got := links(msg)
		if len(got) == 0 || !got[len(got)-1].opaque {
			t.Fatalf("a %d-unit authority gave %d links, the last not opaque", maxDestination+8, len(got))
		}
		if !linksRule(LinksBlockList, "unrelated.example").matchesLinks(msg) || !linksRule(LinksAllowList, "allowed.example").matchesLinks(msg) {
			t.Error("an authority the client may cut anywhere passed a list")
		}
	}
	// Counted as JavaScript counts: a character outside the BMP is two units.
	astral := "https://" + strings.Repeat("\U0001F600", maxDestination/2) + ".allowed.example"
	if got := links(astral); len(got) == 0 || !got[len(got)-1].opaque {
		t.Error("an authority of 2000 UTF-16 units in 1000 characters was not opaque")
	}
	// A long path is no long authority: the cut then falls behind the host.
	fits := "https://allowed.example/" + strings.Repeat("a", 3*maxDestination)
	if linksRule(LinksAllowList, "allowed.example").matchesLinks(fits) {
		t.Error("an allowed host with a long path was refused")
	}
}

// The scan of one message is bounded: when its budget is used up the message
// counts as carrying a link that may lead anywhere.
func TestAScanThatRunsOutOfWorkLeadsAnywhere(t *testing.T) {
	msg := strings.Repeat("https://\u03b1.example*", 300)
	sc := newScan(msg)
	for sc.spend(linkWork / 8) {
	}
	if sc.spend(1) {
		t.Fatal("the budget did not run out")
	}
	got := links(strings.Repeat(msg, 12))
	if len(got) == 0 || !got[len(got)-1].opaque {
		t.Error("a message past the work budget carried no opaque link")
	}
}
