//go:build integration

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/msch128/mnema-talk/internal/config"
)

// restart builds a second server on the same database, as after a restart of
// the process: nothing held in memory survives.
func (a *app) restart() *app {
	a.t.Helper()
	a.router.Close()
	b := &app{t: a.t, db: a.db, store: a.store, events: a.events}
	b.srv = httptest.NewUnstartedServer(nil)
	cfg, err := config.FromEnv(func(k string) (string, bool) {
		v, ok := map[string]string{
			"DATABASE_URL": "unused", "PUBLIC_URL": "http://" + b.srv.Listener.Addr().String(),
			"JWT_SECRET": "integration-test-secret-integration-test",
		}[k]
		return v, ok
	})
	if err != nil {
		a.t.Fatal(err)
	}
	b.router, err = NewRouter(Deps{Config: cfg, DB: a.db, Store: a.store, Events: a.events})
	if err != nil {
		a.t.Fatal(err)
	}
	a.t.Cleanup(b.router.Close)
	b.srv.Config.Handler = b.router
	b.srv.Start()
	a.t.Cleanup(b.srv.Close)
	return b
}

func (a *app) plantFailures(userID, kind string, n int, age string) {
	a.t.Helper()
	_, err := a.db.Exec(context.Background(), `INSERT INTO second_factor_failures (user_id, kind, at)
		SELECT $1::uuid, $2, NOW() - $3::interval FROM generate_series(1, $4)`, userID, kind, age, n)
	if err != nil {
		a.t.Fatal(err)
	}
}

func TestParallelWrongCodesCannotMultiplyTheGuesses(t *testing.T) {
	a := newApp(t, false)
	alice := a.register(a.seedAdmin(), "alice")
	e := a.enroll(alice, memberPassword)

	const attempts = 50
	challenges := make([]string, attempts)
	for i := range challenges {
		challenges[i] = a.plantChallenge(alice.user.ID)
	}
	var wg sync.WaitGroup
	statuses := make([]int, attempts)
	start := make(chan struct{})
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			statuses[i] = secondStep(a.anon(), challenges[i], wrongCode(i+1, e.auth)).status
		}()
	}
	close(start)
	wg.Wait()

	wrong, limited := 0, 0
	for _, s := range statuses {
		switch s {
		case http.StatusUnauthorized:
			wrong++
		case http.StatusTooManyRequests:
			limited++
		default:
			t.Fatalf("unexpected status %d", s)
		}
	}
	// The lockout allows ten wrong codes; every other request is refused
	// without a code being checked.
	if wrong > 10 || limited < attempts-10 {
		t.Fatalf("%d codes were checked, %d requests refused", wrong, limited)
	}
	if n := a.countRows(`SELECT COUNT(*) FROM second_factor_failures`); n > 10 {
		t.Fatalf("%d verifications ran", n)
	}
}

func TestOneChallengeAllowsOnlyFiveParallelCodes(t *testing.T) {
	a := newApp(t, false)
	alice := a.register(a.seedAdmin(), "alice")
	e := a.enroll(alice, memberPassword)
	ch := a.plantChallenge(alice.user.ID)

	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			secondStep(a.anon(), ch, wrongCode(i+1, e.auth))
		}()
	}
	wg.Wait()
	if n := a.countRows(`SELECT COUNT(*) FROM second_factor_failures`); n != 5 {
		t.Fatalf("a challenge is worth five attempts, %d ran", n)
	}
}

func TestTenWrongCodesLockTheSecondStep(t *testing.T) {
	a := newApp(t, false)
	alice := a.register(a.seedAdmin(), "alice")
	e := a.enroll(alice, memberPassword)
	a.freshStep()
	for i := 0; i < 10; i++ {
		secondStep(a.anon(), a.plantChallenge(alice.user.ID), wrongCode(i+1, e.auth))
	}
	res := secondStep(a.anon(), a.plantChallenge(alice.user.ID), e.auth.code(0))
	if res.status != http.StatusTooManyRequests || res.header.Get("Retry-After") == "" {
		t.Fatalf("locked: %d %v", res.status, res.header)
	}
}

func TestLockedAuthenticatorStepNeverBlocksARecoveryCode(t *testing.T) {
	a := newApp(t, false)
	alice := a.register(a.seedAdmin(), "alice")
	e := a.enroll(alice, memberPassword)
	a.freshStep()
	// Somebody who knows the password locks the authenticator step.
	for i := 0; i < 10; i++ {
		secondStep(a.anon(), a.plantChallenge(alice.user.ID), wrongCode(i+1, e.auth))
	}
	if res := secondStep(a.anon(), a.plantChallenge(alice.user.ID), e.auth.code(0)); res.status != http.StatusTooManyRequests {
		t.Fatalf("authenticator step must be locked: %d", res.status)
	}
	if res := secondStep(a.anon(), a.plantChallenge(alice.user.ID), e.recovery[0]); res.status != http.StatusOK {
		t.Fatalf("a recovery code must still sign in: %d %s", res.status, res.body)
	}
}

func TestWrongRecoveryCodesLockOnlyTheClientThatGuesses(t *testing.T) {
	a := newApp(t, false)
	alice := a.register(a.seedAdmin(), "alice")
	e := a.enroll(alice, memberPassword)
	a.freshStep()
	step := func(ip, challenge, code string) response {
		return a.anon().do(http.MethodPost, "/api/auth/login/2fa", map[string]string{"challenge": challenge, "code": code},
			map[string]string{"X-Forwarded-For": ip})
	}
	for i := 0; i < 5; i++ {
		step("198.51.100.7", a.plantChallenge(alice.user.ID), "abcdefghjkmn")
	}
	if res := step("198.51.100.7", a.plantChallenge(alice.user.ID), e.recovery[0]); res.status != http.StatusTooManyRequests {
		t.Fatalf("the guessing client is limited: %d", res.status)
	}
	if res := step("203.0.113.9", a.plantChallenge(alice.user.ID), e.recovery[0]); res.status != http.StatusOK {
		t.Fatalf("another client is not: %d %s", res.status, res.body)
	}
}

func TestTheDailyCapRefusesAuthenticatorCodesButNotRecoveryCodes(t *testing.T) {
	a := newApp(t, false)
	alice := a.register(a.seedAdmin(), "alice")
	e := a.enroll(alice, memberPassword)
	a.freshStep()
	a.plantFailures(alice.user.ID.String(), "totp", 20, "1 hour")

	res := secondStep(a.anon(), a.plantChallenge(alice.user.ID), e.auth.code(0))
	if res.status != http.StatusTooManyRequests || res.header.Get("Retry-After") == "" || !strings.Contains(string(res.body), "recovery code") {
		t.Fatalf("the right authenticator code must be refused at the cap: %d %s", res.status, res.body)
	}
	if res := secondStep(a.anon(), a.plantChallenge(alice.user.ID), e.recovery[0]); res.status != http.StatusOK {
		t.Fatalf("a recovery code must still work: %d %s", res.status, res.body)
	}
	// The cap is a rolling window: once the failures are a day old it lifts.
	if _, err := a.db.Exec(context.Background(), `UPDATE second_factor_failures SET at = NOW() - INTERVAL '25 hours'`); err != nil {
		t.Fatal(err)
	}
	if res := secondStep(a.anon(), a.plantChallenge(alice.user.ID), e.auth.code(0)); res.status != http.StatusOK {
		t.Fatalf("after the window: %d %s", res.status, res.body)
	}
}

func TestRecoveryCodeGuessesHaveTheirOwnBudget(t *testing.T) {
	a := newApp(t, false)
	alice := a.register(a.seedAdmin(), "alice")
	e := a.enroll(alice, memberPassword)
	a.freshStep()
	// A spent authenticator budget does not use up the recovery budget ...
	a.plantFailures(alice.user.ID.String(), "totp", 20, "1 hour")
	if res := secondStep(a.anon(), a.plantChallenge(alice.user.ID), e.recovery[1]); res.status != http.StatusOK {
		t.Fatalf("%d", res.status)
	}
	// ... but guessing recovery codes is bounded too.
	a.plantFailures(alice.user.ID.String(), "recovery", 100, "1 hour")
	if res := secondStep(a.anon(), a.plantChallenge(alice.user.ID), e.recovery[2]); res.status != http.StatusTooManyRequests {
		t.Fatalf("recovery guesses are capped: %d", res.status)
	}
}

func TestTheFailureCountSurvivesARestart(t *testing.T) {
	a := newApp(t, false)
	alice := a.register(a.seedAdmin(), "alice")
	e := a.enroll(alice, memberPassword)
	a.freshStep()
	for i := 0; i < 10; i++ {
		secondStep(a.anon(), a.plantChallenge(alice.user.ID), wrongCode(i+1, e.auth))
	}

	b := a.restart()
	if n := b.countRows(`SELECT COUNT(*) FROM second_factor_failures`); n != 10 {
		t.Fatalf("persisted failures: %d", n)
	}
	// The in-memory lockout is gone, the daily count is not.
	for i := 0; i < 10; i++ {
		secondStep(b.anon(), b.plantChallenge(alice.user.ID), wrongCode(i+20, e.auth))
	}
	c := b.restart()
	res := secondStep(c.anon(), c.plantChallenge(alice.user.ID), e.auth.code(0))
	if res.status != http.StatusTooManyRequests || !strings.Contains(string(res.body), "recovery code") {
		t.Fatalf("after two restarts the right code must still hit the cap: %d %s", res.status, res.body)
	}
}

func TestFiveWrongCodesRaiseANoticeAndOneAlertMail(t *testing.T) {
	a, ms := mailApp(t)
	admin := a.seedAdmin()
	alice := a.confirmedMember(admin, ms, "alice", "alice@example.org")
	e := a.enroll(alice, memberPassword)
	a.freshStep()

	for i := 0; i < 7; i++ {
		secondStep(a.anon(), a.plantChallenge(alice.user.ID), wrongCode(i+1, e.auth))
	}
	mails := ms.WaitFor(t, 2)
	a.settle()
	if len(ms.Messages()) != 2 || !strings.Contains(mails[1].Subject, "wrong sign-in codes") || mails[1].To != "alice@example.org" {
		t.Fatalf("exactly one alert mail expected: %+v", ms.Messages())
	}
	if strings.Contains(mails[1].Body, "http") {
		t.Fatalf("the alert carries no link: %s", mails[1].Body)
	}
	if n := a.countRows(`SELECT COUNT(*) FROM audit_log WHERE action = 'auth.two_factor_alert'`); n != 1 {
		t.Fatalf("audit alert entries: %d", n)
	}
	if n := a.countRows(`SELECT COUNT(*) FROM audit_log WHERE details::text ~ '[0-9]{6}'`); n != 0 {
		t.Fatalf("the audit log must not hold codes: %d", n)
	}

	res := secondStep(a.anon(), a.plantChallenge(alice.user.ID), e.auth.code(1))
	var out struct {
		SecurityNotice *struct {
			FailedAttempts int `json:"failed_attempts"`
		} `json:"security_notice"`
	}
	if res.status != http.StatusOK || json.Unmarshal(res.body, &out) != nil || out.SecurityNotice == nil || out.SecurityNotice.FailedAttempts != 7 {
		t.Fatalf("the notice appears at the next sign-in: %d %s", res.status, res.body)
	}
	a.freshStep()
	res = secondStep(a.anon(), a.plantChallenge(alice.user.ID), e.auth.code(1))
	if res.status != http.StatusOK || strings.Contains(string(res.body), "security_notice") {
		t.Fatalf("shown once: %d %s", res.status, res.body)
	}
}

func TestFewerThanFiveMistakesRaiseNothing(t *testing.T) {
	a, ms := mailApp(t)
	alice := a.confirmedMember(a.seedAdmin(), ms, "alice", "alice@example.org")
	e := a.enroll(alice, memberPassword)
	a.freshStep()
	for i := 0; i < 4; i++ {
		secondStep(a.anon(), a.plantChallenge(alice.user.ID), wrongCode(i+1, e.auth))
	}
	res := secondStep(a.anon(), a.plantChallenge(alice.user.ID), e.auth.code(1))
	if res.status != http.StatusOK || strings.Contains(string(res.body), "security_notice") || len(ms.Messages()) != 1 {
		t.Fatalf("%d %s mails=%d", res.status, res.body, len(ms.Messages()))
	}
}
