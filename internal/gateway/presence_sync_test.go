package gateway

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

type presenceLog struct {
	events []string
}

func (l *presenceLog) attach(g *Gateway) {
	g.OnPresence = func(_ uuid.UUID, online bool) {
		if online {
			l.events = append(l.events, "online")
		} else {
			l.events = append(l.events, "offline")
		}
	}
	g.OnStatus = func(_ uuid.UUID, status string, a *Activity) {
		if a != nil {
			status += ":" + a.Name
		}
		l.events = append(l.events, status)
	}
}

func (l *presenceLog) take() []string {
	out := l.events
	l.events = nil
	return out
}

func sameEvents(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestStatusSurvivesAResumeWithinTheWindow(t *testing.T) {
	g := New(Config{})
	var log presenceLog
	log.attach(g)
	user := uuid.New()
	s := &session{id: newSessionID(), botID: uuid.New(), userID: user, conn: newConnection(nil)}
	if !g.register(s) {
		t.Fatal("register")
	}
	g.setPresence(user, Presence{Status: StatusDND, Activity: &Activity{Name: "work", Type: ActivityPlaying}})
	g.syncPresence()
	if got := log.take(); !sameEvents(got, []string{"dnd:work", "online", "dnd:work"}) {
		t.Fatalf("first connect: %v", got)
	}

	// The connection drops: the bot is offline, but the status is kept.
	s.detach(s.conn)
	g.syncPresence()
	if got := log.take(); !sameEvents(got, []string{"offline"}) {
		t.Fatalf("after the disconnect: %v", got)
	}
	g.mu.Lock()
	_, kept := g.pres[user]
	g.mu.Unlock()
	if !kept {
		t.Fatal("the status must survive until the resume window ends")
	}

	// RESUME within the window: the status comes back with the bot.
	s.mu.Lock()
	s.conn = newConnection(nil)
	s.mu.Unlock()
	g.syncPresence()
	if got := log.take(); !sameEvents(got, []string{"online", "dnd:work"}) {
		t.Fatalf("after the resume: %v", got)
	}

	// The window runs out: the session and the status go.
	s.detach(s.conn)
	g.syncPresence()
	log.take()
	s.mu.Lock()
	s.detachedAt = time.Now().Add(-time.Hour)
	s.mu.Unlock()
	g.expire()
	g.syncPresence()
	g.mu.Lock()
	_, kept = g.pres[user]
	g.mu.Unlock()
	if kept {
		t.Fatal("the status must end with the resume window")
	}
}

func TestLateStatusAfterTheLastSessionLeavesNoEntry(t *testing.T) {
	g := New(Config{})
	var log presenceLog
	log.attach(g)
	user := uuid.New()
	s := &session{id: newSessionID(), botID: uuid.New(), userID: user, conn: newConnection(nil)}
	if !g.register(s) {
		t.Fatal("register")
	}
	g.mu.Lock()
	g.removeLocked(s.id, s)
	g.mu.Unlock()

	g.setPresence(user, Presence{Status: StatusIdle})
	g.syncPresence()
	g.mu.Lock()
	n := len(g.pres)
	g.mu.Unlock()
	if n != 0 || len(log.events) != 0 {
		t.Fatalf("a status set after the last session ended: pres=%d events=%v", n, log.events)
	}
}

func TestIdentifyWithoutPresenceDropsTheOldSessionsStatus(t *testing.T) {
	g := New(Config{})
	var log presenceLog
	log.attach(g)
	user, botID := uuid.New(), uuid.New()
	old := &session{id: newSessionID(), botID: botID, userID: user, detachedAt: time.Now()}
	if !g.register(old) {
		t.Fatal("register")
	}
	g.mu.Lock()
	g.pres = map[uuid.UUID]Presence{user: {Status: StatusDND}}
	g.mu.Unlock()

	fresh := &session{id: newSessionID(), botID: botID, userID: user, conn: newConnection(nil)}
	if !g.register(fresh) {
		t.Fatal("register")
	}
	g.resetIdlePresence(fresh)
	if got := log.take(); !sameEvents(got, []string{"online"}) {
		t.Fatalf("reset: %v", got)
	}
	g.mu.Lock()
	_, kept := g.pres[user]
	g.mu.Unlock()
	if kept {
		t.Fatal("the old status must be gone")
	}

	// With another connection live, the shared status stays.
	g.setPresence(user, Presence{Status: StatusIdle})
	log.take()
	second := &session{id: newSessionID(), botID: botID, userID: user, conn: newConnection(nil)}
	if !g.register(second) {
		t.Fatal("register")
	}
	g.resetIdlePresence(second)
	if got := log.take(); len(got) != 0 {
		t.Fatalf("a live connection's status was reset: %v", got)
	}
}
