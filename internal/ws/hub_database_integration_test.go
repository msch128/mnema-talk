//go:build integration

package ws

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/testutil"
)

func TestMain(m *testing.M) { os.Exit(testutil.Main(m)) }

func wsFixtureUser(t *testing.T, p *db.Pool) auth.User {
	t.Helper()
	u := auth.User{ID: uuid.New(), Username: "ws-" + uuid.NewString()[:20], DisplayName: "WebSocket fixture"}
	if _, err := p.Exec(context.Background(), `INSERT INTO users(id,username,display_name,password_hash,role) VALUES($1,$2,$3,'unused-test-hash','user')`, u.ID, u.Username, u.DisplayName); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := p.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, u.ID); err != nil {
			t.Error(err)
		}
	})
	return u
}

func closedWSFixturePool(t *testing.T, p *db.Pool) *db.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), p.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	return &db.Pool{Pool: pool}
}

func TestWSDatabaseBookkeepingAndChannelChecks(t *testing.T) {
	p := testutil.DB(t)
	u := wsFixtureUser(t, p)
	h := NewHub(p, nil, nil, nil)
	c := testClient(h, u)
	observer := testClient(h, auth.User{ID: uuid.New()})
	drainTypes(t, observer)
	if tv, err := h.tokenVersion(context.Background(), u.ID); err != nil || tv != 0 {
		t.Fatalf("token version = %d, %v", tv, err)
	}
	for _, kind := range []string{"text", "voice"} {
		id := uuid.New()
		if _, err := p.Exec(context.Background(), `INSERT INTO channels(id,name,type) VALUES($1,$2,$3)`, id, "ws-fixture-channel", kind); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := p.Exec(context.Background(), `DELETE FROM channels WHERE id=$1`, id); err != nil {
				t.Error(err)
			}
		})
		_, ok := h.voiceChannel(context.Background(), id)
		if ok != (kind == "voice") {
			t.Fatalf("%s channel accepted as voice: %v", kind, ok)
		}
		if kind == "voice" {
			payload := []byte(`{"channel_id":"` + id.String() + `"}`)
			c.handle("voice_join", payload)
			if c.currentVoice() == nil || *c.currentVoice() != id {
				t.Fatal("protocol voice_join failed")
			}
			c.handle("voice_leave", nil)
		}
		c.handle("typing", []byte(`{"channel_id":"`+id.String()+`"}`))
		c.handle("typing", []byte(`{"channel_id":"`+id.String()+`"}`)) // Duplicate notices are throttled before lookup.
	}
	c.handle("voice_join", []byte(`{"channel_id":"`+uuid.NewString()+`"}`))
	if _, ok := h.voiceChannel(context.Background(), uuid.New()); ok {
		t.Fatal("unknown channel accepted")
	}
	h.addVoiceTime(u.ID, uuid.New(), 2500*time.Millisecond)
	var total int64
	if err := p.QueryRow(context.Background(), `SELECT voice_seconds FROM users WHERE id=$1`, u.ID).Scan(&total); err != nil || total != 2 {
		t.Fatalf("voice total=%d, %v", total, err)
	}
	if got := sentEvents(t, c, "user_stats"); len(got) != 1 || got[0]["voice_seconds"] != float64(2) {
		t.Fatalf("voice statistics announcement = %+v", got)
	}
	h.unregister(c)
	var seen *time.Time
	if err := p.QueryRow(context.Background(), `SELECT last_seen_at FROM users WHERE id=$1`, u.ID).Scan(&seen); err != nil || seen == nil {
		t.Fatalf("last seen=%v, %v", seen, err)
	}
	broken := NewHub(closedWSFixturePool(t, p), nil, nil, nil)
	b := testClient(broken, u)
	broken.addVoiceTime(u.ID, uuid.New(), time.Second)
	broken.unregister(b)
	if broken.OnlineCount() != 0 || len(sentEvents(t, b, "user_stats")) != 0 {
		t.Fatal("database failure retained connection or invented statistics")
	}
}

// Run the writer's heartbeat and revalidation timers on real sockets with
// shortened intervals (the production values are covered by
// TestHubPacingDefaultsToTheProductionIntervals).
func TestWSWriterPeriodicLifecycle(t *testing.T) {
	p := testutil.DB(t)
	for _, mode := range []string{"revoked", "deleted", "database-unavailable", "valid", "ping-write-failure"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			u := wsFixtureUser(t, p)
			server, remote := websocketPair(t)
			h := NewHub(p, nil, nil, nil)
			h.PingInterval, h.RevalidateInterval = 20*time.Millisecond, 300*time.Millisecond
			c := newClient(h, server, u, 0)
			if mode == "database-unavailable" {
				h.DB = closedWSFixturePool(t, p)
			}
			if mode == "revoked" {
				if _, err := p.Exec(context.Background(), `UPDATE users SET token_version=1 WHERE id=$1`, u.ID); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "deleted" {
				if _, err := p.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, u.ID); err != nil {
					t.Fatal(err)
				}
			}
			done := make(chan struct{})
			go func() { c.writePump(); close(done) }()
			t.Cleanup(func() {
				c.shutdown()
				server.Close()
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Error("writer leaked after cleanup")
				}
			})
			if mode == "ping-write-failure" {
				server.Close()
				select {
				case <-done:
				case <-time.After(h.PingInterval + 5*time.Second):
					t.Fatal("failed heartbeat write did not stop writer")
				}
				return
			}
			var heartbeats atomic.Int32
			remote.SetPingHandler(func(data string) error {
				heartbeats.Add(1)
				return remote.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(time.Second))
			})
			remote.SetReadDeadline(time.Now().Add(h.RevalidateInterval + 10*time.Second))
			if mode == "valid" || mode == "database-unavailable" {
				timer := time.AfterFunc(h.RevalidateInterval+time.Second, func() { c.SendEvent("after-revalidation", nil) })
				defer timer.Stop()
				_, body, err := remote.ReadMessage()
				if err != nil {
					t.Fatalf("non-revoked connection closed: %v", err)
				}
				if string(body) != `{"type":"after-revalidation","payload":null}` {
					t.Fatalf("post-revalidation frame=%s", body)
				}
				select {
				case <-done:
					t.Fatal("non-revoked writer terminated")
				default:
				}
			} else {
				_, _, err := remote.ReadMessage()
				if !websocket.IsCloseError(err, websocket.ClosePolicyViolation) {
					t.Fatalf("revocation close=%v, want policy violation", err)
				}
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Fatal("revoked writer did not terminate")
				}
			}
			if heartbeats.Load() < 1 {
				t.Fatal("writer did not send heartbeat before revalidation")
			}
		})
	}
}

// Shutting down records last seen for everyone still connected while the
// database is open; their own clean-up afterwards no longer writes it.
func TestHubCloseRecordsLastSeenBeforeTheDatabaseCloses(t *testing.T) {
	p := testutil.DB(t)
	first, second := wsFixtureUser(t, p), wsFixtureUser(t, p)
	h := NewHub(p, nil, nil, nil)
	a, b := testClient(h, first), testClient(h, second)
	h.Close()
	for _, u := range []auth.User{first, second} {
		var seen *time.Time
		if err := p.QueryRow(context.Background(), `SELECT last_seen_at FROM users WHERE id=$1`, u.ID).Scan(&seen); err != nil || seen == nil {
			t.Fatalf("last seen of %s = %v, %v", u.Username, seen, err)
		}
	}
	if _, err := p.Exec(context.Background(), `UPDATE users SET last_seen_at = NULL WHERE id = ANY($1)`, []uuid.UUID{first.ID, second.ID}); err != nil {
		t.Fatal(err)
	}
	h.unregister(a)
	h.unregister(b)
	var seen *time.Time
	if err := p.QueryRow(context.Background(), `SELECT last_seen_at FROM users WHERE id=$1`, first.ID).Scan(&seen); err != nil || seen != nil {
		t.Fatalf("clean-up after close wrote last seen again: %v, %v", seen, err)
	}
	broken := NewHub(closedWSFixturePool(t, p), nil, nil, nil)
	testClient(broken, first)
	broken.Close()
	if broken.OnlineCount() != 1 {
		t.Fatal("a failed write changed who is online")
	}
}
