//go:build integration

package server

import (
	"net/http"
	"testing"
)

// The link rules read a message with their own pattern (internal/automod), the
// web client renders it with another (web/src/shared/lib/markdown). Each case
// here is a message the client shows as a link to other.example, posted by an
// ordinary member through the real message route against the rule it should
// break. The vectors and the client side are in
// internal/automod/testdata/link_parity.json and
// web/src/shared/lib/markdown/linkParity.test.ts.
//
// knownBypass asserts the WRONG behaviour that the 2026-11 hunt measured
// (findings H-L3-01..04): the message is stored. It fails the day the server
// refuses the message; then move the case into wantRefused.
type linkCase struct{ finding, msg string }

func knownBypass(t *testing.T, f *automodFixture, rule string, c linkCase) {
	t.Helper()
	res := f.say(f.member, f.general, c.msg)
	if res.status != http.StatusCreated {
		t.Errorf("%s: %s rule now refuses %q (%d): the bypass is fixed, move the case to wantRefused", c.finding, rule, c.msg, res.status)
		return
	}
	var stored struct{ Content string }
	res.decode(t, &stored)
	if stored.Content != c.msg {
		t.Errorf("%s: %q was accepted but stored as %q", c.finding, c.msg, stored.Content)
	}
}

func wantRefused(t *testing.T, f *automodFixture, rule, msg string) {
	t.Helper()
	wantStatus(t, rule+" "+msg, f.say(f.member, f.general, msg), http.StatusBadRequest)
}

// Each list holds the messages that get past the rules named in it today.
var (
	// H-L3-01: the server splits the authority before the browser does.
	quoteInUserInfo = []linkCase{
		{"H-L3-01", "https://allowed.example'@other.example/x"},
		{"H-L3-01", "https://allowed.example\uff02@other.example/x"},
		{"H-L3-01", "https://allowed.example\uff07@other.example/x"},
		{"H-L3-01", "https://allowed.example\uff1c@other.example/x"},
		{"H-L3-01", "https://allowed.example\uff40@other.example/x"},
		{"H-L3-01", "https://allowed.example\uff0f@other.example/x"},
		{"H-L3-01", "[docs](https://allowed.example'@other.example/x)"},
		{"H-L3-01", "<https://allowed.example'@other.example/x>"},
	}
	// H-L3-01 with nothing before the "@": the server sees no link at all.
	noLinkAtAll = []linkCase{
		{"H-L3-01", "https://'@other.example/x"},
		{"H-L3-01", "see https://\uff02@other.example/x"},
		{"H-L3-01", "https://\u0085@other.example/x"},
	}
	// H-L3-02: the client ends the link before the server does.
	clientEndsEarly = []linkCase{
		{"H-L3-02", "**https://other.example**"},
		{"H-L3-02", "||https://other.example||"},
		{"H-L3-02", "~~https://other.example~~"},
		{"H-L3-02", "__https://other.example__"},
		{"H-L3-02", "https://other.example|"},
		{"H-L3-02", "[docs](https://other.example)s"},
		{"H-L3-02", "http://_.other.example/x"},
	}
	// H-L3-02 where the server reads on to an allowed host.
	clientEndsBeforeAllowed = []linkCase{
		{"H-L3-02", "https://other.example\u007f@allowed.example/x"},
		{"H-L3-02", "https://other.example\ufeff.allowed.example/x"},
	}
	// H-L3-03: the server's token swallows a link the client opens on its own.
	nested = []linkCase{
		{"H-L3-03", "https://allowed.example:99999/https://other.example/x"},
	}
	nestedInUnreadable = []linkCase{
		{"H-L3-03", "https://[::1/https://other.example/x"},
	}
	// H-L3-04: the server deletes a mark that makes the browser's host another domain.
	markInHost = []linkCase{
		{"H-L3-04", "https://allowed\u0301.example/x"},
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
	wantStatus(t, "allowed host", f.say(f.member, f.general, "https://allowed.example/x"), http.StatusCreated)
	wantRefused(t, f, "allow list", "https://other.example/x")
	wantRefused(t, f, "allow list", "https://allowed.example@other.example/x")
	for _, c := range cases(quoteInUserInfo, noLinkAtAll, clientEndsBeforeAllowed, nested, markInHost) {
		knownBypass(t, f, "allow list", c)
	}
}

func TestABlockListJudgesTheHostTheClientOpens(t *testing.T) {
	f := newAutomodFixture(t)
	f.addRule(linkRule("block_list", "other.example"))
	wantRefused(t, f, "block list", "https://other.example/x")
	wantRefused(t, f, "block list", "**https://other.example/x**") // the markup sits in the path
	for _, c := range cases(quoteInUserInfo, noLinkAtAll, clientEndsEarly, clientEndsBeforeAllowed, nested, nestedInUnreadable) {
		knownBypass(t, f, "block list", c)
	}
}

func TestBlockAllAndNewAccountsSeeEveryLinkTheClientShows(t *testing.T) {
	f := newAutomodFixture(t)
	id := f.addRule(linkRule("block_all"))
	wantRefused(t, f, "block all", "https://other.example/x")
	for _, c := range noLinkAtAll {
		knownBypass(t, f, "block all", c)
	}
	wantStatus(t, "delete", f.admin.delete("/api/admin/automod/rules/"+id.String()), http.StatusNoContent)
	// The default rule of every new server: accounts under 10 minutes may not post links.
	f.addRule(map[string]any{"kind": "new_account", "name": "New accounts", "enabled": true, "action": "block",
		"config": map[string]any{"max_age_seconds": 600, "restrict": "links"}})
	wantRefused(t, f, "new account", "https://other.example/x")
	for _, c := range noLinkAtAll {
		knownBypass(t, f, "new account", c)
	}
}
