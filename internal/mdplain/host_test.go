package mdplain

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The client's answers (safeHref: the URL standard and linkAuthority.ts),
// recorded by web/src/shared/lib/markdown/hrefParity.test.ts, which fails
// when the client answers otherwise. This port must give the same answer
// wherever it claims to know it, and must know it for every ASCII host that
// is no IPv6 address or punycode.
func TestSafeHrefAgreesWithTheClient(t *testing.T) {
	raw, err := os.ReadFile("testdata/hrefs.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct {
		Href string `json:"href"`
		OK   bool   `json:"ok"`
	}
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	unsure := 0
	for _, v := range vectors {
		p := &parser{budget: 1 << 30}
		ok, sure := p.safeHref(encode(v.Href), false)
		switch {
		case sure && ok != v.OK:
			t.Errorf("safeHref(%q) = %v, the client says %v", v.Href, ok, v.OK)
		case !sure && mustKnow(v.Href):
			t.Errorf("safeHref(%q): not sure, but the host is plain ASCII", v.Href)
		case !sure:
			unsure++
		}
	}
	if len(vectors) < 200 {
		t.Fatalf("only %d vectors", len(vectors))
	}
	t.Logf("%d hrefs, %d left to the browser", len(vectors), unsure)
}

// mustKnow: an http(s) URL whose authority, decoded, is ASCII, with no IPv6 literal and no punycode label.
func mustKnow(href string) bool {
	lower := strings.ToLower(href)
	rest, ok := strings.CutPrefix(lower, "http://")
	if !ok {
		if rest, ok = strings.CutPrefix(lower, "https://"); !ok {
			return true
		}
	}
	authority := rest
	if at := strings.IndexAny(rest, "/?#"); at >= 0 {
		authority = rest[:at]
	}
	decoded, valid := percentDecode(authority)
	return !valid || (isASCII(decoded) && !strings.Contains(authority, "[") && !strings.Contains(authority, "xn--"))
}
