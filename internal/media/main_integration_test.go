//go:build integration

package media

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/testutil"
)

func TestMain(m *testing.M) { os.Exit(testutil.Main(m)) }

type mediaFixture struct {
	p       *db.Pool
	h       *Handler
	store   *failingMediaStore
	user    *auth.User
	channel uuid.UUID
}

func mediaDB(t *testing.T) *mediaFixture {
	t.Helper()
	p := testutil.DB(t)
	testutil.Reset(t, p)
	user := &auth.User{ID: uuid.New(), Username: "media-test", DisplayName: "Media test", Role: "admin"}
	channel := uuid.New()
	execMedia(t, p, `INSERT INTO users(id,username,display_name,password_hash,role) VALUES($1,$2,$3,'test-only-unused-hash','admin')`, user.ID, user.Username, user.DisplayName)
	execMedia(t, p, `INSERT INTO channels(id,name,type) VALUES($1,'media-test','text')`, channel)
	store := &failingMediaStore{MemoryStore: NewMemoryStore()}
	return &mediaFixture{p: p, user: user, channel: channel, store: store, h: &Handler{DB: p, Perms: perm.NewService(p), Store: store, Bucket: "test-media", MaxUploadBytes: 1024, Events: &events.Recorder{}}}
}

func execMedia(t *testing.T, p *db.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := p.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

func (f *mediaFixture) req(r *http.Request, name, value string) *http.Request {
	rc := chi.NewRouteContext()
	if name != "" {
		rc.URLParams.Add(name, value)
	}
	ctx := auth.WithUser(r.Context(), f.user)
	return r.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rc))
}

func (f *mediaFixture) attachment(t *testing.T, key string, avatar bool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	var msg *uuid.UUID
	if !avatar {
		m := uuid.New()
		msg = &m
		execMedia(t, f.p, `INSERT INTO messages(id,channel_id,user_id,content) VALUES($1,$2,$3,'attachment')`, m, f.channel, f.user.ID)
	}
	execMedia(t, f.p, `INSERT INTO media(id,uploader_id,channel_id,message_id,s3_bucket,s3_key,original_filename,mime_type,size_bytes,created_at) VALUES($1,$2,$3,$4,'test-media',$5,'a.txt','text/plain',3,NOW()-INTERVAL '3 days')`, id, f.user.ID, f.channel, msg, key)
	f.store.Put(key, []byte("abc"), time.Now().Add(-72*time.Hour))
	return id
}

func cancelledRequest() *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx, cancel := context.WithCancel(r.Context())
	cancel()
	return r.WithContext(ctx)
}
