//go:build integration

package chat

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/chattest"
	"github.com/msch128/mnema-talk/internal/chat/talks"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/testutil"
)

// talksWorld is the small world the conversation call tests need: two members
// and a Talk manager that counts the voice rooms it closes. The Talk tests
// themselves live in chat/talks.
type talksWorld struct {
	p      *db.Pool
	rec    *events.Recorder
	mgr    *talks.TalkManager
	alice  *auth.User
	bob    *auth.User
	closed atomic.Int32
}

type voiceCloser struct{ w *talksWorld }

func (v voiceCloser) CloseVoiceChannel(uuid.UUID) { v.w.closed.Add(1) }

func newTalksUser(t *testing.T, p *db.Pool, name string) *auth.User {
	t.Helper()
	u := &auth.User{Username: name, DisplayName: name, Role: auth.RoleUser}
	if err := p.QueryRow(context.Background(), `INSERT INTO users (username, display_name, password_hash) VALUES ($1,$1,'x') RETURNING id, created_at`, name).Scan(&u.ID, &u.CreatedAt); err != nil {
		t.Fatal(err)
	}
	return u
}

func newTalksWorld(t *testing.T) *talksWorld {
	t.Helper()
	p := testutil.DB(t)
	testutil.Reset(t, p)
	w := &talksWorld{p: p, rec: &events.Recorder{}}
	w.alice, w.bob = newTalksUser(t, p, "alice_t"), newTalksUser(t, p, "bob_t")
	w.mgr = talks.NewTalkManager(&talks.TalkManager{
		DB: p, Events: w.rec, Perms: perm.NewService(p), Audit: &audit.Log{DB: p},
		Voice: voiceCloser{w}, EmptyGrace: time.Hour, // a Talk nobody joins must not vanish under a slow test
	})
	return w
}

func (w *talksWorld) exists(t *testing.T, id uuid.UUID) bool {
	t.Helper()
	var n int
	if err := w.p.QueryRow(context.Background(), `SELECT count(*) FROM channels WHERE id = $1`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

func talkRequest(u *auth.User, body string, params map[string]string) *http.Request {
	return chattest.Request(u, body, params, "")
}
