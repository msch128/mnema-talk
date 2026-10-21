//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func setRules(t *testing.T, admin *client, text string, reaccept bool) {
	t.Helper()
	res := admin.put("/api/admin/onboarding", map[string]any{
		"community_name": "Test Home", "welcome_text": "Welcome!", "rules_text": text, "require_reacceptance": reaccept,
	})
	wantStatus(t, "set onboarding", res, http.StatusOK)
}

func onboardingState(t *testing.T, c *client) map[string]any {
	t.Helper()
	res := c.get("/api/onboarding")
	wantStatus(t, "get onboarding", res, http.StatusOK)
	var st map[string]any
	res.decode(t, &st)
	return st
}

func acceptRules(t *testing.T, c *client) {
	t.Helper()
	st := onboardingState(t, c)
	wantStatus(t, "accept", c.post("/api/onboarding/accept", map[string]any{"version": st["rules_version"]}), http.StatusNoContent)
}

func TestInvitePreviewLeaksNothingAndTreatsExpiredAsUnknown(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	setRules(t, admin, "", false)

	mk := func(body map[string]any) string {
		res := admin.post("/api/admin/invites", body)
		wantStatus(t, "create invite", res, http.StatusCreated)
		var inv struct{ Code string }
		res.decode(t, &inv)
		return inv.Code
	}
	good := mk(map[string]any{"expires_in_hours": 5})
	open := mk(map[string]any{})
	expired := mk(map[string]any{})
	used := mk(map[string]any{"max_uses": 1})
	if _, err := a.db.Exec(t.Context(), `UPDATE invites SET expires_at = NOW() - INTERVAL '1 minute' WHERE code = $1`, expired); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(t.Context(), `UPDATE invites SET uses_count = 1 WHERE code = $1`, used); err != nil {
		t.Fatal(err)
	}

	anon := a.anon()
	raw := func(code string) []byte {
		res := anon.get("/api/invites/" + code)
		wantStatus(t, "preview "+code, res, http.StatusOK)
		if res.header.Get("Cache-Control") != "no-store" {
			t.Fatalf("preview is cacheable: %q", res.header.Get("Cache-Control"))
		}
		return res.body
	}

	var fields map[string]any
	if err := json.Unmarshal(raw(good), &fields); err != nil {
		t.Fatal(err)
	}
	if fields["valid"] != true || fields["community_name"] != "Test Home" || fields["expires_at"] == nil || len(fields) != 3 {
		t.Fatalf("valid preview: %v", fields)
	}
	fields = map[string]any{}
	if err := json.Unmarshal(raw(open), &fields); err != nil {
		t.Fatal(err)
	}
	if fields["valid"] != true || len(fields) != 2 {
		t.Fatalf("invite without expiry: %v", fields)
	}

	unknown := string(raw("zzzzzzzzzzzz"))
	if unknown != `{"valid":false}`+"\n" && unknown != `{"valid":false}` {
		t.Fatalf("unknown code: %s", unknown)
	}
	for name, code := range map[string]string{"expired": expired, "used up": used, "malformed": "a b", "too short": "abc"} {
		if got := string(raw(code)); got != unknown {
			t.Fatalf("%s differs from unknown: %s", name, got)
		}
	}
	// Revoked is gone from the table, hence unknown too.
	var list []struct {
		ID   string
		Code string
	}
	admin.get("/api/admin/invites").decode(t, &list)
	for _, inv := range list {
		if inv.Code == open {
			wantStatus(t, "revoke", admin.delete("/api/admin/invites/"+inv.ID), http.StatusNoContent)
		}
	}
	if got := string(raw(open)); got != unknown {
		t.Fatalf("revoked differs from unknown: %s", got)
	}
}

func TestInvitePreviewIsRateLimitedPerIP(t *testing.T) {
	a := newApp(t, false)
	anon := a.anon()
	limited := 0
	for range 40 {
		if res := anon.get("/api/invites/guess-guess-1"); res.status == http.StatusTooManyRequests {
			limited++
			if res.code(t) != "RATE_LIMITED" || res.header.Get("Retry-After") == "" {
				t.Fatalf("limited without code or Retry-After: %s", res.body)
			}
		}
	}
	if limited == 0 {
		t.Fatal("guessing invite codes is not limited")
	}
}

func TestOnboardingSettingsAreAdminOnly(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	member := a.register(owner, "mia")
	body := map[string]any{"community_name": "x", "welcome_text": "", "rules_text": "rules"}
	wantStatus(t, "member reads", member.get("/api/admin/onboarding"), http.StatusForbidden)
	wantStatus(t, "member writes", member.put("/api/admin/onboarding", body), http.StatusForbidden)
	wantStatus(t, "anonymous", a.anon().get("/api/admin/onboarding"), http.StatusUnauthorized)

	voice := a.createChannel(owner, "Lounge", "voice")
	bad := map[string]any{"community_name": "x", "welcome_text": "", "rules_text": "", "start_channel_id": voice}
	wantStatus(t, "voice as start channel", owner.put("/api/admin/onboarding", bad), http.StatusBadRequest)
	bad["start_channel_id"] = uuid.New()
	wantStatus(t, "unknown start channel", owner.put("/api/admin/onboarding", bad), http.StatusBadRequest)
	wantStatus(t, "unknown field", owner.put("/api/admin/onboarding", map[string]any{"rules": "x"}), http.StatusBadRequest)
	wantStatus(t, "settings readable by the owner", owner.get("/api/admin/onboarding"), http.StatusOK)
}

func TestEmptyRulesEnforceNothing(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	member := a.register(owner, "mia")
	ch := a.createChannel(owner, "general", "text")

	st := onboardingState(t, member)
	if st["rules_pending"] != false || st["rules_version"].(float64) != 0 {
		t.Fatalf("fresh instance: %v", st)
	}
	a.send(member, ch, "hello")
	me := member.get("/api/auth/me")
	if strings.Contains(string(me.body), "rules_pending") {
		t.Fatalf("/auth/me claims pending rules: %s", me.body)
	}
	// Welcome text and a name alone enforce nothing either.
	setRules(t, owner, "", true)
	a.send(member, ch, "still fine")
	// Nothing to accept: accepting version 0 is a no-op, any other is stale.
	wantStatus(t, "accept nothing", member.post("/api/onboarding/accept", map[string]any{"version": 0}), http.StatusNoContent)
	wantStatus(t, "stale accept", member.post("/api/onboarding/accept", map[string]any{"version": 1}), http.StatusConflict)
}

func TestPendingRulesBlockWritesEverywhereButNotTheOwner(t *testing.T) {
	a := newApp(t, true)
	owner := a.seedAdmin()
	old := a.register(owner, "olivia") // registered before any rules existed
	ch := a.createChannel(owner, "general", "text")
	talk := a.createChannel(owner, "Lounge", "voice")
	msg := a.send(owner, ch, "read me")

	setRules(t, owner, "# Rules\nBe kind.", false)
	// The first publication needs everyone's acceptance, the Owner excepted.
	st := onboardingState(t, old)
	if st["rules_pending"] != true || st["rules_version"].(float64) != 1 || st["rules_text"] != "# Rules\nBe kind." {
		t.Fatalf("pending state: %v", st)
	}
	if onboardingState(t, owner)["rules_pending"] != false {
		t.Fatal("the Owner is never pending")
	}
	var me struct {
		RulesPending bool `json:"rules_pending"`
	}
	old.get("/api/auth/me").decode(t, &me)
	if !me.RulesPending {
		t.Fatal("/auth/me must carry rules_pending")
	}

	// REST: reading stays, every write answers RULES_NOT_ACCEPTED.
	wantStatus(t, "read", old.get("/api/channels/"+ch.String()+"/messages"), http.StatusOK)
	denied := map[string]response{
		"send":   old.post("/api/channels/"+ch.String()+"/messages", map[string]any{"content": "hi"}),
		"react":  old.post("/api/messages/"+msg.String()+"/reactions", map[string]any{"emoji": "👍"}),
		"upload": old.upload("/api/channels/"+ch.String()+"/upload", "file", "a.png", "image/png", pngBytes, nil),
	}
	for name, res := range denied {
		if res.status != http.StatusForbidden || res.code(t) != "RULES_NOT_ACCEPTED" {
			t.Fatalf("%s: %d %s", name, res.status, res.body)
		}
	}
	a.send(owner, ch, "the Owner is exempt")

	// Talk: the hub refuses the join with the reason.
	oldWS, ownerWS := mustDial(t, old), mustDial(t, owner)
	oldWS.send("voice_join", map[string]any{"channel_id": talk})
	p, ok := oldWS.expect("voice_kicked", soon)
	if !ok || !strings.Contains(string(p), "rules_not_accepted") {
		t.Fatalf("join while pending: %s", p)
	}
	ownerWS.send("voice_join", map[string]any{"channel_id": talk})
	if _, ok := ownerWS.expect("voice_state_update", soon); !ok {
		t.Fatal("the Owner joins a Talk")
	}

	// A stale version is refused, the right one lifts everything.
	wantStatus(t, "stale", old.post("/api/onboarding/accept", map[string]any{"version": 7}), http.StatusConflict)
	acceptRules(t, old)
	a.send(old, ch, "now I may")
	wantStatus(t, "react", old.post("/api/messages/"+msg.String()+"/reactions", map[string]any{"emoji": "👍"}), http.StatusOK)
	oldWS.send("voice_join", map[string]any{"channel_id": talk})
	if _, ok := ownerWS.expect("voice_state_update", soon); !ok {
		t.Fatal("a member who accepted joins a Talk")
	}
}

func TestNewMembersMustAcceptAndReacceptanceRaisesTheVersion(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	ch := a.createChannel(owner, "general", "text")
	setRules(t, owner, "v1 rules", false)

	// Registered while rules are in force: pending from the start, even though
	// the permission state was cached before this account existed.
	newcomer := a.register(owner, "nora")
	res := newcomer.post("/api/channels/"+ch.String()+"/messages", map[string]any{"content": "first!"})
	if res.status != http.StatusForbidden || res.code(t) != "RULES_NOT_ACCEPTED" {
		t.Fatalf("newcomer posts: %d %s", res.status, res.body)
	}
	if !newcomer.user.RulesPending && onboardingState(t, newcomer)["rules_pending"] != true {
		t.Fatal("newcomer not pending")
	}
	acceptRules(t, newcomer)
	a.send(newcomer, ch, "hello")

	// An edit alone keeps acceptances; require_reacceptance does not.
	setRules(t, owner, "v1 rules, typo fixed", false)
	a.send(newcomer, ch, "still allowed")
	setRules(t, owner, "v2 rules", true)
	st := onboardingState(t, newcomer)
	if st["rules_pending"] != true || st["rules_version"].(float64) != 2 || st["rules_accepted_version"].(float64) != 1 {
		t.Fatalf("after re-acceptance was required: %v", st)
	}
	wantStatus(t, "blocked again", newcomer.post("/api/channels/"+ch.String()+"/messages", map[string]any{"content": "x"}), http.StatusForbidden)
	acceptRules(t, newcomer)
	a.send(newcomer, ch, "ok")

	// Clearing the rules lifts everything for everybody.
	setRules(t, owner, "", false)
	other := a.register(owner, "otto")
	a.send(other, ch, "no rules, no gate")
}

func TestTipsAreStoredPerMemberAndNeverForgotten(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	mia := a.register(owner, "mia")
	ben := a.register(owner, "ben")

	res := mia.put("/api/onboarding/tips", map[string]any{"dismissed_tips": []string{"voice", "search"}, "welcome_done": true})
	wantStatus(t, "dismiss", res, http.StatusOK)
	wantStatus(t, "second device", mia.put("/api/onboarding/tips", map[string]any{"dismissed_tips": []string{"search", "mentions"}}), http.StatusOK)
	st := onboardingState(t, mia)
	tips, _ := json.Marshal(st["dismissed_tips"])
	if string(tips) != `["voice","search","mentions"]` || st["welcome_done"] != true {
		t.Fatalf("tips: %s %v", tips, st["welcome_done"])
	}
	if got := onboardingState(t, ben); len(got["dismissed_tips"].([]any)) != 0 || got["welcome_done"] != false {
		t.Fatalf("another member's tips leaked: %v", got)
	}
	wantStatus(t, "bad id", mia.put("/api/onboarding/tips", map[string]any{"dismissed_tips": []string{"Bad Id"}}), http.StatusBadRequest)
	wantStatus(t, "unknown field", mia.put("/api/onboarding/tips", map[string]any{"x": 1}), http.StatusBadRequest)
}

func TestStartChannelOnlyForThoseWhoCanSeeIt(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	mia := a.register(owner, "mia")
	ch := a.createChannel(owner, "welcome", "text")
	res := owner.put("/api/admin/onboarding", map[string]any{"community_name": "H", "welcome_text": "", "rules_text": "", "start_channel_id": ch})
	wantStatus(t, "set start channel", res, http.StatusOK)
	if got := onboardingState(t, mia)["start_channel_id"]; got != ch.String() {
		t.Fatalf("start channel: %v", got)
	}
	// Hide it from @everyone: the member no longer learns its id.
	var roles []struct {
		ID         string `json:"id"`
		IsEveryone bool   `json:"is_everyone"`
	}
	owner.get("/api/roles").decode(t, &roles)
	for _, r := range roles {
		if r.IsEveryone {
			wantStatus(t, "hide", owner.put("/api/admin/channels/"+ch.String()+"/overwrites", map[string]any{
				"overwrites": []map[string]any{{"type": "role", "id": r.ID, "allow": 0, "deny": 2}},
			}), http.StatusNoContent)
		}
	}
	if got := onboardingState(t, mia)["start_channel_id"]; got != nil {
		t.Fatalf("hidden start channel leaked: %v", got)
	}
}
