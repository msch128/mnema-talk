//go:build integration

package automod

import (
	"context"
	"net/http"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/testutil"
)

func TestMain(m *testing.M) { os.Exit(testutil.Main(m)) }

func newStore(t *testing.T) *db.Pool {
	t.Helper()
	p := testutil.DB(t)
	testutil.Reset(t, p)
	return p
}

func wantAPIStatus(t *testing.T, what string, err error, status int) {
	t.Helper()
	api, ok := httpx.AsAPIError(err)
	if !ok || api.Status != status {
		t.Fatalf("%s: got %v, want status %d", what, err, status)
	}
}

// normalized is what the handlers pass to the store.
func normalized(in RuleInput) RuleInput {
	out, err := in.Normalize()
	if err != nil {
		panic(err)
	}
	return out
}

func wordsInput(name string, words ...string) RuleInput {
	return normalized(RuleInput{Kind: KindWords, Name: name, Enabled: true, Action: ActionBlock, Config: Settings{Words: words}})
}

func raidInput() RuleInput {
	return normalized(RuleInput{Kind: KindRaid, Name: "Raid", Enabled: true, Action: ActionFlag,
		Config: Settings{Joins: 5, WindowSeconds: 30, RestrictSeconds: 600}})
}

func version(t *testing.T, p *db.Pool) int64 {
	t.Helper()
	_, v, err := ListRules(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestRulesAreStoredListedOldestFirstAndBumpTheVersion(t *testing.T) {
	p := newStore(t)
	ctx := context.Background()
	rules, v0, err := ListRules(ctx, p)
	if err != nil || rules == nil || len(rules) != 0 {
		t.Fatalf("empty set = %v, %v", rules, err)
	}

	role, channel := uuid.New(), uuid.New()
	in := wordsInput("first", "spam")
	in.ExemptRoles, in.ExemptChannels = []uuid.UUID{role}, []uuid.UUID{channel}
	first, err := CreateRule(ctx, p, in)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CreateRule(ctx, p, wordsInput("second", "scam"))
	if err != nil {
		t.Fatal(err)
	}

	rules, v2, err := ListRules(ctx, p)
	if err != nil || len(rules) != 2 || rules[0].ID != first.ID || rules[1].ID != second.ID {
		t.Fatalf("rules = %+v, %v", rules, err)
	}
	if v2 != v0+2 {
		t.Fatalf("version %d after two creates from %d", v2, v0)
	}
	if got := rules[0]; got.Config.Words[0] != "spam" || got.ExemptRoles[0] != role || got.ExemptChannels[0] != channel {
		t.Fatalf("stored rule = %+v", got)
	}
	if got := rules[1]; got.ExemptRoles == nil || got.ExemptChannels == nil {
		t.Fatalf("a rule without exemptions must list empty arrays, got %+v", got)
	}
}

func TestOnlyOneRaidRuleAndAtMostMaxRulesExist(t *testing.T) {
	p := newStore(t)
	ctx := context.Background()
	if _, err := CreateRule(ctx, p, raidInput()); err != nil {
		t.Fatal(err)
	}
	_, err := CreateRule(ctx, p, raidInput())
	wantAPIStatus(t, "second raid rule", err, http.StatusConflict)

	for i := 1; i < MaxRules; i++ {
		if _, err := CreateRule(ctx, p, wordsInput("w", "x")); err != nil {
			t.Fatalf("rule %d: %v", i+1, err)
		}
	}
	before := version(t, p)
	_, err = CreateRule(ctx, p, wordsInput("one too many", "x"))
	wantAPIStatus(t, "rule above the limit", err, http.StatusConflict)
	if after := version(t, p); after != before {
		t.Fatalf("a refused create bumped the version %d -> %d", before, after)
	}
}

func TestUpdateReplacesARuleButNeverChangesItsKind(t *testing.T) {
	p := newStore(t)
	ctx := context.Background()
	rule, err := CreateRule(ctx, p, wordsInput("old", "spam"))
	if err != nil {
		t.Fatal(err)
	}
	before := version(t, p)

	upd := wordsInput("new", "scam", "fraud")
	upd.Enabled, upd.Action, upd.TimeoutSeconds = false, ActionFlag, 60
	got, err := UpdateRule(ctx, p, rule.ID, upd)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "new" || got.Enabled || got.Action != ActionFlag || got.TimeoutSeconds != 60 || len(got.Config.Words) != 2 {
		t.Fatalf("updated = %+v", got)
	}
	if v := version(t, p); v != before+1 {
		t.Fatalf("version %d, want %d", v, before+1)
	}

	_, err = UpdateRule(ctx, p, rule.ID, raidInput())
	wantAPIStatus(t, "changing the kind", err, http.StatusBadRequest)
	_, err = UpdateRule(ctx, p, uuid.New(), upd)
	wantAPIStatus(t, "unknown rule", err, http.StatusNotFound)
	if v := version(t, p); v != before+1 {
		t.Fatalf("refused updates bumped the version to %d", v)
	}
}

func TestDeleteReturnsTheRemovedRuleAndBumpsTheVersion(t *testing.T) {
	p := newStore(t)
	ctx := context.Background()
	rule, err := CreateRule(ctx, p, wordsInput("doomed", "spam"))
	if err != nil {
		t.Fatal(err)
	}
	before := version(t, p)

	gone, err := DeleteRule(ctx, p, rule.ID)
	if err != nil || gone.ID != rule.ID || gone.Name != "doomed" {
		t.Fatalf("deleted = %+v, %v", gone, err)
	}
	rules, v, err := ListRules(ctx, p)
	if err != nil || len(rules) != 0 || v != before+1 {
		t.Fatalf("after delete: %d rules, version %d (was %d), %v", len(rules), v, before, err)
	}
	_, err = DeleteRule(ctx, p, rule.ID)
	wantAPIStatus(t, "second delete", err, http.StatusNotFound)
}

func TestStoreCallsFailCleanlyOnACanceledRequest(t *testing.T) {
	p := newStore(t)
	rule, err := CreateRule(context.Background(), p, wordsInput("kept", "spam"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := map[string]func() error{
		"list":   func() error { _, _, err := ListRules(ctx, p); return err },
		"create": func() error { _, err := CreateRule(ctx, p, wordsInput("x", "y")); return err },
		"update": func() error { _, err := UpdateRule(ctx, p, rule.ID, wordsInput("x", "y")); return err },
		"delete": func() error { _, err := DeleteRule(ctx, p, rule.ID); return err },
	}
	for name, call := range calls {
		if call() == nil {
			t.Errorf("%s succeeded on a canceled request", name)
		}
	}
	rules, _, err := ListRules(context.Background(), p)
	if err != nil || len(rules) != 1 || rules[0].Name != "kept" {
		t.Fatalf("rules after canceled calls = %+v, %v", rules, err)
	}
}
