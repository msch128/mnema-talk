//go:build integration

package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
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
	return &mediaFixture{p: p, user: user, channel: channel, store: store, h: &Handler{DB: p, Store: store, Bucket: "test-media", MaxUploadBytes: 1024, Events: &events.Recorder{}}}
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

func TestMediaHandlersWithoutStorage(t *testing.T) {
	h := &Handler{}
	for name, handler := range map[string]func(http.ResponseWriter, *http.Request) error{"upload": h.upload, "avatar": h.uploadAvatar, "serve": h.serve, "prune": h.prune, "delete": h.delete, "orphans": h.orphans, "cleanup": h.cleanupOrphans} {
		t.Run(name, func(t *testing.T) {
			requireAPIError(t, handler(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil)), 503)
		})
	}
}

func TestMediaUploadValidationAndFailedDatabaseWrite(t *testing.T) {
	f := mediaDB(t)
	for _, id := range []string{"bad", uuid.NewString()} {
		err := f.h.upload(httptest.NewRecorder(), f.req(httptest.NewRequest(http.MethodPost, "/", nil), "channelID", id))
		if id == "bad" {
			requireAPIError(t, err, 400)
		} else {
			requireAPIError(t, err, 404)
		}
	}
	err := f.h.upload(httptest.NewRecorder(), f.req(httptest.NewRequest(http.MethodPost, "/", strings.NewReader("bad")), "channelID", f.channel.String()))
	requireAPIError(t, err, 400)
	// A concurrent account deletion after storage completes must roll back the message and discard its object.
	f.store.afterUpload = func() { execMedia(t, f.p, `DELETE FROM users WHERE id=$1`, f.user.ID) }
	r := f.req(uploadRequest(t, formPart{name: "file", filename: "a.txt", mime: "text/plain", value: "abc"}), "channelID", f.channel.String())
	err = f.h.upload(httptest.NewRecorder(), r)
	if err == nil || !strings.Contains(err.Error(), "record upload") || f.store.Len() != 0 {
		t.Fatalf("failed insert: %v, objects=%d", err, f.store.Len())
	}
}

func TestValidateUploadTargetLoadsThreadAndRejectsMissingTarget(t *testing.T) {
	f := mediaDB(t)
	root := uuid.New()
	execMedia(t, f.p, `INSERT INTO messages(id,channel_id,user_id,content) VALUES($1,$2,$3,'root')`, root, f.channel, f.user.ID)
	target, err := f.h.validateTarget(context.Background(), f.channel, map[string]string{"parent_id": root.String(), "reply_to_id": root.String()})
	if err != nil || target.parentID == nil || *target.parentID != root || target.replyToID == nil || *target.replyToID != root {
		t.Fatalf("thread root quote=%+v %v", target, err)
	}
	target, err = f.h.validateTarget(context.Background(), f.channel, map[string]string{"parent_id": root.String()})
	if err != nil || target.parentID == nil || *target.parentID != root {
		t.Fatalf("valid thread=%+v %v", target, err)
	}
	_, err = f.h.validateTarget(context.Background(), f.channel, map[string]string{"parent_id": uuid.NewString()})
	requireAPIError(t, err, 400)
}

func TestAvatarStorageAndDatabaseFailures(t *testing.T) {
	f := mediaDB(t)
	request := func() *http.Request {
		return f.req(uploadRequest(t, formPart{name: "avatar", filename: "a.png", mime: "image/png", value: string(pngBytes)}), "", "")
	}
	f.store.uploadErr = errors.New("store rejected upload")
	if err := f.h.uploadAvatar(httptest.NewRecorder(), request()); err == nil || !strings.Contains(err.Error(), "store avatar") {
		t.Fatalf("storage failure=%v", err)
	}
	f.store.uploadErr = nil
	f.store.afterUpload = func() { execMedia(t, f.p, `DELETE FROM users WHERE id=$1`, f.user.ID) }
	if err := f.h.uploadAvatar(httptest.NewRecorder(), request()); err == nil || !strings.Contains(err.Error(), "record avatar") || f.store.Len() != 0 {
		t.Fatalf("missing avatar user=%v objects=%d", err, f.store.Len())
	}
}

func TestAvatarTransactionRollsBackRejectedMediaInsert(t *testing.T) {
	f := mediaDB(t)
	execMedia(t, f.p, `CREATE FUNCTION reject_test_media() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'media write rejected'; END $$`)
	execMedia(t, f.p, `CREATE TRIGGER reject_test_media BEFORE INSERT ON media FOR EACH ROW EXECUTE FUNCTION reject_test_media()`)
	t.Cleanup(func() {
		execMedia(t, f.p, `DROP TRIGGER reject_test_media ON media`)
		execMedia(t, f.p, `DROP FUNCTION reject_test_media()`)
	})
	r := f.req(uploadRequest(t, formPart{name: "avatar", filename: "a.png", mime: "image/png", value: string(pngBytes)}), "", "")
	if err := f.h.uploadAvatar(httptest.NewRecorder(), r); err == nil || !strings.Contains(err.Error(), "media write rejected") || f.store.Len() != 0 {
		t.Fatalf("rejected media insert=%v objects=%d", err, f.store.Len())
	}
	var previous *string
	if err := f.p.QueryRow(context.Background(), `SELECT avatar_s3_key FROM users WHERE id=$1`, f.user.ID).Scan(&previous); err != nil || previous != nil {
		t.Fatalf("rolled-back avatar=%v %v", previous, err)
	}
}

func TestAvatarReplacementDeleteFailureDoesNotUndoNewAvatar(t *testing.T) {
	f := mediaDB(t)
	old := f.attachment(t, "avatars/old.png", true)
	execMedia(t, f.p, `UPDATE users SET avatar_s3_key=$1 WHERE id=$2`, old.String(), f.user.ID)
	f.store.deleteErr = errors.New("old object delete unavailable")
	r := f.req(uploadRequest(t, formPart{name: "avatar", filename: "a.png", mime: "image/png", value: string(pngBytes)}), "", "")
	w := httptest.NewRecorder()
	if err := f.h.uploadAvatar(w, r); err != nil || w.Code != 200 || f.store.Len() != 2 {
		t.Fatalf("replacement=%v code=%d objects=%d", err, w.Code, f.store.Len())
	}
	f.store.deleteErr = nil
	// Deleting the account concurrently with cleanup must be reported when reloading the updated user.
	f.store.afterDelete = func() { execMedia(t, f.p, `DELETE FROM users WHERE id=$1`, f.user.ID) }
	r = f.req(uploadRequest(t, formPart{name: "avatar", filename: "b.png", mime: "image/png", value: string(pngBytes)}), "", "")
	if err := f.h.uploadAvatar(httptest.NewRecorder(), r); err == nil {
		t.Fatal("missing updated user was not reported")
	}
}

func TestMediaServingAndDashboardQueryFailures(t *testing.T) {
	f := mediaDB(t)
	for name, handler := range map[string]func(http.ResponseWriter, *http.Request) error{"stats": f.h.stats, "list": f.h.list, "serve": f.h.serve, "delete": f.h.delete, "prune": f.h.prune} {
		t.Run(name, func(t *testing.T) {
			r := f.req(cancelledRequest(), "id", uuid.NewString())
			r.URL.RawQuery = "days=1"
			if err := handler(httptest.NewRecorder(), r); err == nil {
				t.Fatal("canceled query succeeded")
			}
		})
	}
	for _, handler := range []func(http.ResponseWriter, *http.Request) error{f.h.serve, f.h.delete} {
		requireAPIError(t, handler(httptest.NewRecorder(), f.req(httptest.NewRequest(http.MethodGet, "/", nil), "id", "invalid")), 400)
	}
	requireAPIError(t, f.h.serve(httptest.NewRecorder(), f.req(httptest.NewRequest(http.MethodGet, "/", nil), "id", uuid.NewString())), 404)
	f.h.RetentionDays = 0
	for _, days := range []string{"", "0", "3651", "bad"} {
		r := httptest.NewRequest(http.MethodPost, "/?days="+days, nil)
		requireAPIError(t, f.h.prune(httptest.NewRecorder(), r), 400)
	}
	// Query normalization clamps a negative dashboard offset to zero.
	id := f.attachment(t, "uploads/list", false)
	w := httptest.NewRecorder()
	if err := f.h.list(w, httptest.NewRequest(http.MethodGet, "/?offset=-4", nil)); err != nil || !strings.Contains(w.Body.String(), id.String()) {
		t.Fatalf("negative offset dashboard=%s %v", w.Body.String(), err)
	}
}

type getFailureStore struct {
	Store
	err error
}

func (s getFailureStore) GetObjectFrom(context.Context, string, int64) (io.ReadCloser, error) {
	return nil, s.err
}

func TestMediaServeStorageFailure(t *testing.T) {
	f := mediaDB(t)
	id := f.attachment(t, "uploads/a", false)
	sentinel := errors.New("object storage unavailable")
	f.h.Store = getFailureStore{Store: f.store, err: sentinel}
	r := f.req(httptest.NewRequest(http.MethodGet, "/", nil), "id", id.String())
	if err := f.h.serve(httptest.NewRecorder(), r); !errors.Is(err, sentinel) || !strings.Contains(err.Error(), "fetch media object") {
		t.Fatalf("fetch failure=%v", err)
	}
}

func TestDeleteMediaFailuresAndIdempotence(t *testing.T) {
	f := mediaDB(t)
	id := f.attachment(t, "uploads/a", false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := DeleteMedia(ctx, f.p, f.store, id); err == nil || !strings.Contains(err.Error(), "load media") {
		t.Fatalf("canceled delete=%v", err)
	}
	requireAPIError(t, DeleteMedia(context.Background(), f.p, f.store, uuid.New()), 404)
	f.store.deleteErr = errors.New("storage refused delete")
	if err := DeleteMedia(context.Background(), f.p, f.store, id); err == nil || !strings.Contains(err.Error(), "delete object") {
		t.Fatalf("object failure=%v", err)
	}
	f.store.deleteErr = nil
	if err := DeleteMedia(context.Background(), f.p, f.store, id); err != nil {
		t.Fatal(err)
	}
	if err := DeleteMedia(context.Background(), f.p, f.store, id); err != nil {
		t.Fatal(err)
	}
	if f.store.Has("uploads/a") {
		t.Fatal("deleted object remains")
	}
}

func TestPruneFailuresLeaveAccurateDatabaseState(t *testing.T) {
	f := mediaDB(t)
	id := f.attachment(t, "uploads/old", false)
	requireAPIError(t, func() error { _, err := PruneOlderThan(context.Background(), f.p, f.store, 0); return err }(), 400)
	f.store.batchErr = errors.New("storage refused delete")
	if n, err := PruneOlderThan(context.Background(), f.p, f.store, 1); n != 0 || err == nil || !f.store.Has("uploads/old") {
		t.Fatalf("failed prune n=%d %v", n, err)
	}
	f.store.batchErr = nil
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.store.afterBatch = cancel
	if n, err := PruneOlderThan(ctx, f.p, f.store, 1); n != 0 || err == nil || !strings.Contains(err.Error(), "mark pruned media") {
		t.Fatalf("failed mark n=%d %v", n, err)
	}
	var deleted bool
	if err := f.p.QueryRow(context.Background(), `SELECT is_deleted FROM media WHERE id=$1`, id).Scan(&deleted); err != nil || deleted {
		t.Fatalf("mark persisted after canceled query=%v %v", deleted, err)
	}
}

func TestFindOrphansBatchesAndReportsStorageFailures(t *testing.T) {
	f := mediaDB(t)
	f.attachment(t, "uploads/known", false)
	for i := 0; i < orphanBatch+1; i++ {
		f.store.Put("uploads/"+uuid.NewString(), []byte("abc"), time.Now().Add(-72*time.Hour))
	}
	f.store.Put("uploads/fresh", []byte("fresh"), time.Now())
	f.store.Put("avatars/orphan", []byte("avatar"), time.Now().Add(-72*time.Hour))
	f.store.Put("unrelated/old", []byte("other"), time.Now().Add(-72*time.Hour))
	result, err := FindOrphans(context.Background(), f.p, f.store, true)
	if err != nil || result.Count != orphanBatch+2 || result.Deleted != orphanBatch+2 || result.Bytes != 3*(orphanBatch+1)+6 || !f.store.Has("uploads/fresh") || !f.store.Has("uploads/known") || !f.store.Has("unrelated/old") {
		t.Fatalf("orphans=%+v %v", result, err)
	}
	f.store.listErr = errors.New("list unavailable")
	if _, err := FindOrphans(context.Background(), f.p, f.store, false); !errors.Is(err, f.store.listErr) {
		t.Fatalf("list failure=%v", err)
	}
	if err := f.h.orphans(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil)); err == nil {
		t.Fatal("orphan handler lost scan failure")
	}
	f.store.listErr = nil
	f.store.Put("uploads/orphan", []byte("abc"), time.Now().Add(-72*time.Hour))
	f.store.batchErr = errors.New("delete unavailable")
	if o, err := FindOrphans(context.Background(), f.p, f.store, true); err == nil || !strings.Contains(err.Error(), "delete orphaned objects") || o.Deleted != 0 || !f.store.Has("uploads/orphan") {
		t.Fatalf("delete failure=%+v %v", o, err)
	}
	if err := f.h.cleanupOrphans(httptest.NewRecorder(), f.req(httptest.NewRequest(http.MethodPost, "/", nil), "", "")); err == nil {
		t.Fatal("cleanup lost delete failure")
	}
	f.store.batchErr = nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := FindOrphans(ctx, f.p, f.store, false); err == nil || !strings.Contains(err.Error(), "look up media keys") {
		t.Fatalf("lookup failure=%v", err)
	}
}

func TestScheduledMediaTasksUseRealDatabaseAndRemainDefaultOff(t *testing.T) {
	var log bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	f := mediaDB(t)
	f.attachment(t, "uploads/old", false)
	retentionTask(f.p, f.store, 1)(context.Background())
	if f.store.Has("uploads/old") {
		t.Fatal("scheduled prune left old attachment")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	retentionTask(f.p, f.store, 1)(ctx)
	orphanScanTask(f.p, f.store)(context.Background())
	f.store.Put("avatars/orphan", []byte("old"), time.Now().Add(-72*time.Hour))
	orphanScanTask(f.p, f.store)(context.Background())
	if !f.store.Has("avatars/orphan") {
		t.Fatal("scheduled orphan scan deleted data")
	}
	f.store.listErr = errors.New("scan unavailable")
	orphanScanTask(f.p, f.store)(context.Background())
	StartRetentionWorker(ctx, f.p, f.store, 1)
	StartOrphanScan(ctx, f.p, f.store)
	StartOrphanScan(ctx, f.p, nil)
	for _, expected := range []string{"media pruned", "media pruning failed", "orphaned media objects found", "orphaned media scan failed"} {
		if !strings.Contains(log.String(), expected) {
			t.Fatalf("scheduled task did not report %q: %s", expected, log.String())
		}
	}
	if retentionFirstRun != time.Minute || orphanScanFirstRun != 10*time.Minute || retentionInterval != 24*time.Hour {
		t.Fatal("worker production schedule changed")
	}
}

func TestMountedMediaRoutesServeUploadAndAdminLifecycle(t *testing.T) {
	f := mediaDB(t)
	old := f.attachment(t, "uploads/old", false)
	f.attachment(t, "avatars/retained", true)
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), f.user)))
		})
	})
	f.h.MountUploads(r)
	f.h.Mount(r)
	r.Route("/admin", f.h.MountAdmin)
	call := func(method, path string, req *http.Request) *httptest.ResponseRecorder {
		if req == nil {
			req = httptest.NewRequest(method, path, nil)
		}
		req.Method = method
		req.URL.Path = strings.TrimPrefix(strings.Split(path, "?")[0], "/api")
		if strings.Contains(path, "?") {
			req.URL.RawQuery = strings.SplitN(path, "?", 2)[1]
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	if got := f.h.UploadBodyLimit(); got != 1024+1<<20 {
		t.Fatalf("body ceiling=%d", got)
	}
	w := call(http.MethodPost, "/channels/"+f.channel.String()+"/upload", uploadRequest(t, formPart{name: "file", filename: "notes.txt", mime: "text/plain", value: "notes"}, formPart{name: "content", value: "caption"}))
	if w.Code != 201 {
		t.Fatalf("upload=%d %s", w.Code, w.Body.String())
	}
	var msg struct {
		Attachments []struct {
			ID  uuid.UUID `json:"id"`
			URL string    `json:"url"`
		} `json:"attachments"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &msg); err != nil || len(msg.Attachments) != 1 {
		t.Fatalf("attachment response=%s %v", w.Body.String(), err)
	}
	id := msg.Attachments[0].ID
	w = call(http.MethodGet, msg.Attachments[0].URL, nil)
	if w.Code != 200 || w.Body.String() != "notes" || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment") || w.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("served=%d %q headers=%v", w.Code, w.Body.String(), w.Header())
	}
	req := httptest.NewRequest(http.MethodGet, msg.Attachments[0].URL, nil)
	req.Header.Set("If-None-Match", w.Header().Get("ETag"))
	if w := call(http.MethodGet, msg.Attachments[0].URL, req); w.Code != 304 {
		t.Fatalf("conditional GET=%d", w.Code)
	}
	w = call(http.MethodGet, "/admin/media/stats", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"total_files":3`) {
		t.Fatalf("stats=%d %s", w.Code, w.Body.String())
	}
	w = call(http.MethodGet, "/admin/media", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), id.String()) {
		t.Fatalf("list=%d %s", w.Code, w.Body.String())
	}
	w = call(http.MethodPost, "/admin/media/prune?days=1", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"pruned_count":1`) || f.store.Has("uploads/old") || !f.store.Has("avatars/retained") {
		t.Fatalf("prune=%d %s", w.Code, w.Body.String())
	}
	if w := call(http.MethodGet, "/media/"+old.String(), nil); w.Code != 410 {
		t.Fatalf("pruned attachment=%d", w.Code)
	}
	f.store.Put("avatars/orphan", []byte("old"), time.Now().Add(-72*time.Hour))
	w = call(http.MethodGet, "/admin/media/orphans", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"count":1`) {
		t.Fatalf("orphan count=%d %s", w.Code, w.Body.String())
	}
	w = call(http.MethodPost, "/admin/media/orphans/cleanup", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"deleted":1`) || f.store.Has("avatars/orphan") {
		t.Fatalf("orphan cleanup=%d %s", w.Code, w.Body.String())
	}
	if w := call(http.MethodDelete, "/admin/media/"+id.String(), nil); w.Code != 204 {
		t.Fatalf("delete=%d %s", w.Code, w.Body.String())
	}
	if w := call(http.MethodPost, "/users/me/avatar", uploadRequest(t, formPart{name: "avatar", filename: "a.png", mime: "image/png", value: string(pngBytes)})); w.Code != 200 {
		t.Fatalf("avatar=%d %s", w.Code, w.Body.String())
	}
	recorded := f.h.Events.(*events.Recorder).Snapshot()
	if len(recorded) != 2 || recorded[0].Type != "message_create" || recorded[1].Type != "user_update" {
		t.Fatalf("events=%+v", recorded)
	}
}

func TestDashboardRejectsUnrepresentablePostgresTimestamp(t *testing.T) {
	f := mediaDB(t)
	id := f.attachment(t, "uploads/infinite-date", false)
	// PostgreSQL permits infinity for timestamptz; the Go dashboard requires a finite time.Time.
	execMedia(t, f.p, `UPDATE media SET created_at='infinity'::timestamptz WHERE id=$1`, id)
	if items, err := ListMedia(context.Background(), f.p, 10, 0); err == nil || items != nil {
		t.Fatalf("infinite dashboard timestamp: items=%v error=%v", items, err)
	}
}

func TestPrunePagesPastMaximumBatch(t *testing.T) {
	f := mediaDB(t)
	message := uuid.New()
	execMedia(t, f.p, `INSERT INTO messages(id,channel_id,user_id,content) VALUES($1,$2,$3,'old files')`, message, f.channel, f.user.ID)
	execMedia(t, f.p, `INSERT INTO media(uploader_id,channel_id,message_id,s3_bucket,s3_key,original_filename,mime_type,size_bytes,created_at)
 SELECT $1,$2,$3,'test-media','uploads/batch-' || n::text,'a.txt','text/plain',3,NOW()-INTERVAL '3 days' FROM generate_series(1,$4) n`, f.user.ID, f.channel, message, pruneBatch+1)
	for n := 1; n <= pruneBatch+1; n++ {
		f.store.Put("uploads/batch-"+strconv.Itoa(n), []byte("old"), time.Now().Add(-72*time.Hour))
	}
	n, err := PruneOlderThan(context.Background(), f.p, f.store, 1)
	if err != nil || n != pruneBatch+1 || f.store.Len() != 0 {
		t.Fatalf("batched prune=%d error=%v objects=%d", n, err, f.store.Len())
	}
	if n, err := PruneOlderThan(context.Background(), f.p, f.store, 1); err != nil || n != 0 {
		t.Fatalf("empty prune=%d %v", n, err)
	}
	stats, err := GetStats(context.Background(), f.p)
	if err != nil || stats.DeletedFiles != pruneBatch+1 || stats.TotalFiles != 0 {
		t.Fatalf("pruned stats=%+v %v", stats, err)
	}
}

func TestMediaStorageMissingAndSafeInlineImage(t *testing.T) {
	f := mediaDB(t)
	id := f.attachment(t, "uploads/missing", false)
	if err := f.store.Delete(context.Background(), "uploads/missing"); err != nil {
		t.Fatal(err)
	}
	requireAPIError(t, f.h.serve(httptest.NewRecorder(), f.req(httptest.NewRequest(http.MethodGet, "/", nil), "id", id.String())), 410)
	execMedia(t, f.p, `UPDATE media SET mime_type='image/png',size_bytes=$1 WHERE id=$2`, len(pngBytes), id)
	f.store.Put("uploads/missing", pngBytes, time.Now())
	w := httptest.NewRecorder()
	if err := f.h.serve(w, f.req(httptest.NewRequest(http.MethodGet, "/", nil), "id", id.String())); err != nil || w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "inline") || w.Body.String() != string(pngBytes) {
		t.Fatalf("inline media code=%d error=%v headers=%v", w.Code, err, w.Header())
	}
	r := f.req(uploadRequest(t, formPart{name: "avatar", filename: "a.png", mime: "image/png"}), "", "")
	requireAPIError(t, f.h.uploadAvatar(httptest.NewRecorder(), r), 400)
	r = f.req(uploadRequest(t, formPart{name: "file", filename: "evil.png", mime: "image/png", value: string(htmlBytes)}), "channelID", f.channel.String())
	requireAPIError(t, f.h.upload(httptest.NewRecorder(), r), 415)
}

func TestOrphanScanReportsDatabaseFailureWhileReadingRows(t *testing.T) {
	f := mediaDB(t)
	f.attachment(t, "uploads/known", false)
	// Cache the SELECT on one real connection before taking a conflicting table lock.
	// The subsequent execution fails after Query returns its streaming rows.
	cfg := f.p.Config().Copy()
	cfg.MinConns = 0
	cfg.MaxConns = 1
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "20ms"
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	p := &db.Pool{Pool: pool}
	if _, err := FindOrphans(context.Background(), p, f.store, false); err != nil {
		t.Fatal(err)
	}
	tx, err := f.p.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(context.Background(), `LOCK TABLE media IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	result, err := FindOrphans(context.Background(), p, f.store, true)
	var pgerr *pgconn.PgError
	if !errors.As(err, &pgerr) || pgerr.Code != "55P03" || strings.Contains(err.Error(), "look up media keys") || result.Deleted != 0 || !f.store.Has("uploads/known") {
		t.Fatalf("streamed query failure result=%+v error=%v", result, err)
	}
}
