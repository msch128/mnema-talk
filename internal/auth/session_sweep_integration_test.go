//go:build integration

package auth

import (
	"context"
	"sync"
	"testing"
	"time"
)

// sockets records the sessions whose WebSockets were closed.
type sockets struct {
	LiveControl
	mu    sync.Mutex
	jtis  []string
	count chan struct{}
}

func (s *sockets) DisconnectSession(jti string) {
	s.mu.Lock()
	s.jtis = append(s.jtis, jti)
	s.mu.Unlock()
	select {
	case s.count <- struct{}{}:
	default:
	}
}

func TestSweepWithNothingToDoEndsNothing(t *testing.T) {
	e := newSessionEnv(t)
	active := e.signIn(e.member, firefoxLinux)
	jtis, err := e.s.SweepSessions(context.Background())
	if err != nil || len(jtis) != 0 {
		t.Fatalf("sweep: %v %v", jtis, err)
	}
	if !e.alive(active) {
		t.Fatal("an active session was ended")
	}
}

func TestSweepRunsInTheBackgroundAndClosesTheEndedSessionsSockets(t *testing.T) {
	e := newSessionEnv(t)
	idle := e.signIn(e.member, safariIPhone)
	active := e.signIn(e.member, firefoxLinux)
	e.addPush(e.member.ID, idle)
	e.age(idle, 9*24*time.Hour)

	live := &sockets{count: make(chan struct{}, 1)}
	h := NewHandler(e.s, nil)
	h.Live = live
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { h.RunSessionSweep(ctx, time.Hour); close(done) }()

	select {
	case <-live.count:
	case <-time.After(10 * time.Second):
		t.Fatal("the sweep did not close the idle session's sockets")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the sweep kept running after its context ended")
	}
	if len(live.jtis) != 1 || live.jtis[0] != e.jti(idle) {
		t.Errorf("closed sockets of %v", live.jtis)
	}
	if e.pushCount() != 0 || e.alive(idle) || !e.alive(active) {
		t.Error("expected the idle session and its push device gone, the active one kept")
	}
}

func TestSweepWithoutADatabaseReturnsAtOnce(t *testing.T) {
	done := make(chan struct{})
	go func() { NewHandler(&Sessions{}, nil).RunSessionSweep(context.Background(), 0); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the sweep waited without a database")
	}
}

func TestSweepFailuresLeaveSessionsAndPushDevicesAlone(t *testing.T) {
	for _, query := range []string{
		"DELETE FROM user_sessions WHERE expires_at",
		"DELETE FROM user_sessions WHERE last_active_at",
		"INSERT INTO revoked_sessions",
		"DELETE FROM push_subscriptions WHERE session_id",
	} {
		t.Run(query, func(t *testing.T) {
			e := newSessionEnv(t)
			idle := e.signIn(e.member, safariIPhone)
			e.addPush(e.member.ID, idle)
			e.age(idle, 9*24*time.Hour)

			broken := authHookPool(t, e.p, &authQueryHook{match: query, cancel: true})
			s := &Sessions{DB: broken, Secret: e.s.Secret, TTL: e.s.TTL}
			if _, err := s.SweepSessions(context.Background()); err == nil {
				t.Fatal("a failed sweep reported success")
			}
			// The sweep is one transaction: nothing was half done.
			var rows int
			if err := e.p.QueryRow(context.Background(), `SELECT COUNT(*) FROM user_sessions`).Scan(&rows); err != nil || rows != 1 {
				t.Errorf("session rows %d %v, want the untouched 1", rows, err)
			}
			if e.pushCount() != 1 {
				t.Error("the push device was removed by a failed sweep")
			}
		})
	}
}
