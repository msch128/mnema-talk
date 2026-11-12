//go:build integration

package server

import (
	"net/http"
	"testing"
)

// The link rules read a message with their own scanner (internal/automod), the
// web client renders it with its Markdown parser (web/src/shared/lib/markdown).
// Each case here is a message the client shows as a link to other.example (or
// a host outside allowed.example), posted by an ordinary member through the
// real message route against the rule it breaks: the server must refuse it.
// The vectors and the client side are in
// internal/automod/testdata/link_parity.json and
// web/src/shared/lib/markdown/linkParity.test.ts. The groups are the
// bypasses of the 2026-11 hunt (H-L3-01..04) and their review.
type linkCase struct{ finding, msg string }

func wantRefusedCase(t *testing.T, f *automodFixture, rule string, c linkCase) {
	t.Helper()
	if res := f.say(f.member, f.general, c.msg); res.status != http.StatusBadRequest {
		t.Errorf("%s: %s rule let %q through (%d)", c.finding, rule, c.msg, res.status)
	}
}

func wantRefused(t *testing.T, f *automodFixture, rule, msg string) {
	t.Helper()
	wantStatus(t, rule+" "+msg, f.say(f.member, f.general, msg), http.StatusBadRequest)
}

func wantAccepted(t *testing.T, f *automodFixture, rule, msg string) {
	t.Helper()
	if res := f.say(f.member, f.general, msg); res.status != http.StatusCreated {
		t.Errorf("%s rule refused the allowed %q (%d)", rule, msg, res.status)
	}
}

var (
	// H-L3-01: a character the server ended the authority at; the browser
	// reads what comes before the "@" as user info.
	quoteInUserInfo = []linkCase{
		{"H-L3-01", "https://allowed.example'@other.example/x"},
		{"H-L3-01", "https://allowed.example\uff02@other.example/x"},
		{"H-L3-01", "https://allowed.example\uff07@other.example/x"},
		{"H-L3-01", "https://allowed.example\uff1c@other.example/x"},
		{"H-L3-01", "https://allowed.example\uff1e@other.example/x"},
		{"H-L3-01", "https://allowed.example\uff1f@other.example/x"},
		{"H-L3-01", "https://allowed.example\uff03@other.example/x"},
		{"H-L3-01", "https://allowed.example\uff3c@other.example/x"},
		{"H-L3-01", "https://allowed.example\uff40@other.example/x"},
		{"H-L3-01", "https://allowed.example\uff0f@other.example/x"},
		{"H-L3-01", "[docs](https://allowed.example'@other.example/x)"},
		{"H-L3-01", "<https://allowed.example'@other.example/x>"},
	}
	// H-L3-01 with nothing before the "@": the server once saw no link at all.
	noLinkAtAll = []linkCase{
		{"H-L3-01", "https://'@other.example/x"},
		{"H-L3-01", "see https://\uff02@other.example/x"},
		{"H-L3-01", "https://\u0085@other.example/x"},
	}
	// H-L3-02: the client ends the link before the server did.
	clientEndsEarly = []linkCase{
		{"H-L3-02", "**https://other.example**"},
		{"H-L3-02", "*https://other.example*"},
		{"H-L3-02", "||https://other.example||"},
		{"H-L3-02", "~~https://other.example~~"},
		{"H-L3-02", "__https://other.example__"},
		{"H-L3-02", "https://other.example|"},
		{"H-L3-02", "[docs](https://other.example)s"},
		{"H-L3-02", "http://_.other.example/x"},
		{"H-L3-02", "http://a-.other.example/x"},
	}
	// H-L3-02 where the server read on to an allowed host.
	clientEndsBeforeAllowed = []linkCase{
		{"H-L3-02", "https://other.example\u007f@allowed.example/x"},
		{"H-L3-02", "https://other.example\u0008@allowed.example/y"},
		{"H-L3-02", "https://other.example\ufeff.allowed.example/x"},
		{"H-L3-02", "**https://other.example**.allowed.example"},
	}
	// H-L3-03: the client rejects the outer link and links one inside it.
	nested = []linkCase{
		{"H-L3-03", "https://allowed.example:99999/https://other.example/x"},
		{"H-L3-03", "https://allowed.example:70000/https://other.example/z"},
		{"H-L3-03", "www.allowed.example/r/https://other.example/x"},
	}
	nestedInUnreadable = []linkCase{
		{"H-L3-03", "https://[::1/https://other.example/x"},
	}
	// H-L3-04: a mark the server folded away, which makes the browser's host another domain.
	markInHost = []linkCase{
		{"H-L3-04", "https://allowed\u0301.example/x"},
		{"H-L3-04", "https://allowed\u030a.example/q"},
	}
	// Links to allowed.example only, in the forms members write: no rule about other hosts may refuse them.
	allowedForms = []string{
		"https://allowed.example/x", "(https://allowed.example)", "[docs](https://allowed.example/x)", "<https://allowed.example>",
		"**https://allowed.example**", "||https://allowed.example||", "__https://allowed.example__", "https://allowed.example!",
		"see https://docs.allowed.example/guide?a=1#b.", "https://allowed.example's page", "www.allowed.example",
	}
)

func cases(lists ...[]linkCase) []linkCase {
	var out []linkCase
	for _, l := range lists {
		out = append(out, l...)
	}
	return out
}

func linkRule(mode string, domains ...string) map[string]any {
	return map[string]any{"kind": "links", "name": "Links " + mode, "enabled": true, "action": "block",
		"config": map[string]any{"mode": mode, "domains": domains}}
}

func TestAnAllowListJudgesTheHostTheClientOpens(t *testing.T) {
	f := newAutomodFixture(t)
	f.addRule(linkRule("allow_list", "allowed.example"))
	for _, msg := range allowedForms {
		wantAccepted(t, f, "allow list", msg)
	}
	wantRefused(t, f, "allow list", "https://other.example/x")
	wantRefused(t, f, "allow list", "https://allowed.example@other.example/x")
	for _, c := range cases(quoteInUserInfo, noLinkAtAll, clientEndsBeforeAllowed, nested, markInHost) {
		wantRefusedCase(t, f, "allow list", c)
	}
}

func TestABlockListJudgesTheHostTheClientOpens(t *testing.T) {
	f := newAutomodFixture(t)
	f.addRule(linkRule("block_list", "other.example"))
	for _, msg := range allowedForms {
		wantAccepted(t, f, "block list", msg)
	}
	wantRefused(t, f, "block list", "https://other.example/x")
	wantRefused(t, f, "block list", "**https://other.example/x**") // the markup sits in the path
	for _, c := range cases(quoteInUserInfo, noLinkAtAll, clientEndsEarly, clientEndsBeforeAllowed, nested, nestedInUnreadable) {
		wantRefusedCase(t, f, "block list", c)
	}
}

func TestBlockAllAndNewAccountsSeeEveryLinkTheClientShows(t *testing.T) {
	f := newAutomodFixture(t)
	id := f.addRule(linkRule("block_all"))
	wantRefused(t, f, "block all", "https://other.example/x")
	for _, c := range noLinkAtAll {
		wantRefusedCase(t, f, "block all", c)
	}
	wantStatus(t, "delete", f.admin.delete("/api/admin/automod/rules/"+id.String()), http.StatusNoContent)
	// The default rule of every new server: accounts under 10 minutes may not post links.
	f.addRule(map[string]any{"kind": "new_account", "name": "New accounts", "enabled": true, "action": "block",
		"config": map[string]any{"max_age_seconds": 600, "restrict": "links"}})
	wantRefused(t, f, "new account", "https://other.example/x")
	for _, c := range noLinkAtAll {
		wantRefusedCase(t, f, "new account", c)
	}
}
