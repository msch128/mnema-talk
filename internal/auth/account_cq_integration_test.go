//go:build integration

package auth

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/mailer"
	"github.com/msch128/mnema-talk/internal/perm"
)

// archiveFile returns the named file of the one archive in the store.
func archiveFile(t *testing.T, store *memStore, name string) ([]byte, bool) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	for key, data := range store.data {
		if !strings.HasPrefix(key, "exports/") {
			continue
		}
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range zr.File {
			if f.Name != name {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer rc.Close()
			b, err := io.ReadAll(rc)
			if err != nil {
				t.Fatal(err)
			}
			return b, true
		}
	}
	return nil, false
}

func TestExportShowsChannelNamesToTheOwner(t *testing.T) {
	h, pool, _, store, channel := exportFixture(t)
	var ownerID uuid.UUID
	if err := pool.QueryRow(context.Background(), `SELECT id FROM users WHERE role = 'admin'`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	seedMessages(t, pool, ownerID, channel, 1)
	h.Perms = perm.NewService(pool)
	if _, err := runExport(t, h, ownerID); err != nil {
		t.Fatal(err)
	}
	raw, ok := archiveFile(t, store, "messages.json")
	if !ok {
		t.Fatal("no messages.json")
	}
	var doc struct {
		Messages []struct {
			ChannelName *string `json:"channel_name"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil || len(doc.Messages) != 1 {
		t.Fatalf("messages.json: %v %s", err, raw)
	}
	if doc.Messages[0].ChannelName == nil || *doc.Messages[0].ChannelName != "room" {
		t.Fatalf("the Owner sees every channel, got %v", doc.Messages[0].ChannelName)
	}
}

func TestExportNamesNoChannelWithoutAPermissionService(t *testing.T) {
	h, pool, member, store, channel := exportFixture(t)
	seedMessages(t, pool, member.ID, channel, 1)
	if _, err := runExport(t, h, member.ID); err != nil {
		t.Fatal(err)
	}
	raw, _ := archiveFile(t, store, "messages.json")
	if !strings.Contains(string(raw), `"channel_name":null`) {
		t.Fatalf("channel name leaked: %s", raw)
	}
}

func TestExportListsEveryListThatWasCutAtTheRowBound(t *testing.T) {
	old := exportMaxRows
	exportMaxRows = 3
	t.Cleanup(func() { exportMaxRows = old })
	h, pool, member, store, channel := exportFixture(t)
	seedMessages(t, pool, member.ID, channel, 4)
	if _, err := pool.Exec(context.Background(), `INSERT INTO message_reactions (message_id, user_id, emoji)
		SELECT id, $1, 'x' FROM messages WHERE user_id = $1`, member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := runExport(t, h, member.ID); err != nil {
		t.Fatal(err)
	}
	raw, _ := archiveFile(t, store, "reactions.json")
	var reactions []json.RawMessage
	if err := json.Unmarshal(raw, &reactions); err != nil || len(reactions) != 3 {
		t.Fatalf("reactions.json: %v, %d rows", err, len(reactions))
	}
	raw, ok := archiveFile(t, store, "truncated.json")
	if !ok {
		t.Fatal("a cut list must be named in truncated.json")
	}
	var cut struct {
		Files []string `json:"files"`
	}
	if err := json.Unmarshal(raw, &cut); err != nil || len(cut.Files) != 1 || cut.Files[0] != "reactions.json" {
		t.Fatalf("truncated.json: %v %s", err, raw)
	}
}

func TestExportWithoutCutListsHasNoTruncatedFile(t *testing.T) {
	h, _, member, store, _ := exportFixture(t)
	if _, err := runExport(t, h, member.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := archiveFile(t, store, "truncated.json"); ok {
		t.Fatal("nothing was cut")
	}
}

func seedEmoji(t *testing.T, pool *db.Pool, user uuid.UUID, name, key string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `INSERT INTO custom_emoji (name, content_type, s3_key, size_bytes, content_hash, created_by)
		VALUES ($1, 'image/png', $2, 3, $1, $3)`, name, key, user); err != nil {
		t.Fatal(err)
	}
}

func TestExportListsEmojiWhoseFileIsGoneButFailsOnOtherStorageErrors(t *testing.T) {
	h, pool, member, store, _ := exportFixture(t)
	h.ObjectMissing = func(err error) bool { return errors.Is(err, errStoreMissing) }
	seedEmoji(t, pool, member.ID, "gone", "emoji/gone.png")
	if _, err := runExport(t, h, member.ID); err != nil {
		t.Fatalf("a missing file must not fail the export: %v", err)
	}
	raw, _ := archiveFile(t, store, "emoji.json")
	if !strings.Contains(string(raw), `"gone"`) || strings.Contains(string(raw), `"file"`) {
		t.Fatalf("emoji.json: %s", raw)
	}

	// A storage failure that is not "no such object" fails the export, so the
	// single-use download is never spent on an incomplete archive.
	flaky := &failingGetStore{memStore: newMemStore(), err: errors.New("timeout")}
	flaky.data["emoji/gone.png"] = []byte("png")
	h.Objects = flaky
	if _, err := runExport(t, h, member.ID); err == nil {
		t.Fatal("a storage timeout must fail the export")
	}
}

// failingGetStore fails every read except exports' own uploads.
type failingGetStore struct {
	*memStore
	err error
}

func (f *failingGetStore) GetObjectFrom(context.Context, string, int64) (io.ReadCloser, error) {
	return nil, f.err
}

func TestExportIncludesAvatarAndBannerAndFailsWhenTheStorageFails(t *testing.T) {
	h, pool, member, store, _ := exportFixture(t)
	h.ObjectMissing = func(err error) bool { return errors.Is(err, errStoreMissing) }
	ctx := context.Background()
	add := func(column, key string, data []byte) {
		var id uuid.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO media (uploader_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes)
			VALUES ($1, 'b', $2, 'x', 'image/png', $3) RETURNING id`, member.ID, key, len(data)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE users SET `+column+` = $2 WHERE id = $1`, member.ID, id.String()); err != nil {
			t.Fatal(err)
		}
		store.data[key] = data
	}
	add("avatar_s3_key", "avatars/a.png", []byte("avatar-bytes"))
	add("banner_s3_key", "banners/b.png", []byte("banner-bytes"))
	if _, err := runExport(t, h, member.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := archiveFile(t, store, "avatar.png"); string(got) != "avatar-bytes" {
		t.Fatalf("avatar.png: %q", got)
	}
	if got, _ := archiveFile(t, store, "banner.png"); string(got) != "banner-bytes" {
		t.Fatalf("banner.png: %q", got)
	}
	raw, _ := archiveFile(t, store, "profile.json")
	if !strings.Contains(string(raw), `"avatar_file": "avatar.png"`) || !strings.Contains(string(raw), `"banner_file": "banner.png"`) {
		t.Fatalf("profile.json does not name the files: %s", raw)
	}

	h.Objects = &failingGetStore{memStore: store, err: errors.New("503 slow down")}
	if _, err := runExport(t, h, member.ID); err == nil {
		t.Fatal("a storage failure while reading the pictures must fail the export")
	}
}

func TestExportRequestWithAllSlotsBusyIsRefusedAndNotRecorded(t *testing.T) {
	h, pool, member, _, _ := exportFixture(t)
	h.Audit = &audit.Log{DB: pool}
	for i := 0; i < cap(h.exportSlots); i++ {
		h.exportSlots <- struct{}{}
	}
	w := authRequest(t, h.requestExport, member, `{"current_password":"`+testPassword+`"}`, uuid.Nil)
	if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") == "" {
		t.Fatalf("%d %s retry-after=%q", w.Code, w.Body, w.Header().Get("Retry-After"))
	}
	var requests, failed int
	if err := pool.QueryRow(context.Background(), `SELECT (SELECT COUNT(*) FROM audit_log WHERE action = 'auth.export_request'),
		(SELECT COUNT(*) FROM data_exports WHERE status = 'failed')`).Scan(&requests, &failed); err != nil {
		t.Fatal(err)
	}
	if requests != 0 || failed != 1 {
		t.Fatalf("audit entries %d, failed exports %d", requests, failed)
	}
	// A failed one does not use up the day's export.
	for i := 0; i < cap(h.exportSlots); i++ {
		<-h.exportSlots
	}
	w = authRequest(t, h.requestExport, member, `{"current_password":"`+testPassword+`"}`, uuid.Nil)
	if w.Code != http.StatusAccepted {
		t.Fatalf("retry: %d %s", w.Code, w.Body)
	}
	h.DrainMail()
}

// stuckStore blocks an upload until its context ends.
type stuckStore struct {
	*memStore
	started chan struct{}
}

func (s *stuckStore) Upload(ctx context.Context, _ string, body io.Reader, _ string, _ int64) error {
	close(s.started)
	<-ctx.Done()
	return ctx.Err()
}

func TestShutdownCancelsExportsInFlightAndWaitsForThem(t *testing.T) {
	h, pool, member, _, _ := exportFixture(t)
	stuck := &stuckStore{memStore: newMemStore(), started: make(chan struct{})}
	h.Objects = stuck
	lifetime, stop := context.WithCancel(context.Background())
	defer stop()
	h.Lifetime = lifetime
	id, _, err := h.claimExport(context.Background(), member.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !h.buildExportInBackground(id, member.ID) {
		t.Fatal("no slot")
	}
	<-stuck.started
	if h.Drain(50 * time.Millisecond) {
		t.Fatal("the export is still running")
	}
	stop()
	if !h.Drain(10 * time.Second) {
		t.Fatal("shutdown must cancel the export instead of waiting for its timeout")
	}
	var status string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM data_exports WHERE id = $1`, id).Scan(&status); err != nil || status != "failed" {
		t.Fatalf("status %q, %v", status, err)
	}
}

// commitTracer runs fn once a transaction has committed.
type commitTracer struct{ fn func() }

func (commitTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return ctx
}

func (c commitTracer) TraceQueryEnd(_ context.Context, _ *pgx.Conn, d pgx.TraceQueryEndData) {
	if d.CommandTag.String() == "COMMIT" {
		c.fn()
	}
}

func tracedPool(t *testing.T, source *db.Pool, tracer pgx.QueryTracer) *db.Pool {
	t.Helper()
	config := source.Config().Copy()
	config.ConnConfig.Tracer = tracer
	config.MinConns = 0
	p, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return &db.Pool{Pool: p}
}

func TestDeletionFinishesItsCleanupWhenTheClientHangsUp(t *testing.T) {
	h, pool, member, store, _ := exportFixture(t)
	if _, err := runExport(t, h, member.ID); err != nil || store.count() != 1 {
		t.Fatalf("export: %v, files %d", err, store.count())
	}
	reqCtx, hangUp := context.WithCancel(context.Background())
	defer hangUp()
	h.Sessions.DB = tracedPool(t, pool, commitTracer{fn: hangUp})
	h.Audit = &audit.Log{DB: pool}

	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"current_password":"`+testPassword+`"}`)).WithContext(reqCtx)
	r = r.WithContext(WithUser(r.Context(), member))
	w := httptest.NewRecorder()
	httpx.Handle(h.deleteAccount).ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if reqCtx.Err() == nil {
		t.Fatal("the test did not hang up")
	}
	if store.count() != 0 {
		t.Fatal("the files of the deleted account are orphans")
	}
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM audit_log WHERE action = 'auth.account_delete'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("audit entries %d, %v", n, err)
	}
}

func TestASecondDeletionRacingTheFirstAnswersNoContent(t *testing.T) {
	pool, _, member := authFixture(t)
	hook := &authQueryHook{
		match: "SELECT username FROM users WHERE id = $1 AND NOT is_bot AND deleted_at IS NULL FOR UPDATE",
		before: func() {
			// The parallel request wins and commits first.
			err := pgx.BeginFunc(context.Background(), pool, func(tx pgx.Tx) error {
				_, err := eraseAccount(context.Background(), tx, member.ID)
				return err
			})
			if err != nil {
				t.Error(err)
			}
		},
	}
	h := NewHandler(&Sessions{DB: authHookPool(t, pool, hook), Secret: "export-test-secret-export-test-secret"}, nil)
	w := authRequest(t, h.deleteAccount, member, `{"current_password":"`+testPassword+`"}`, uuid.Nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

// refusingMail fails every send.
type refusingMail struct{}

func (refusingMail) Send(context.Context, mailer.Message) error { return errors.New("smtp down") }

func TestNewAddressIsKeptAndReportedWhenTheConfirmationMailIsRefused(t *testing.T) {
	pool, _, member := authFixture(t)
	h := NewHandler(&Sessions{DB: pool, Secret: "export-test-secret-export-test-secret"}, nil)
	h.Mail = refusingMail{}
	w := authRequest(t, h.setEmail, member, `{"email":"ada@example.org","current_password":"`+testPassword+`"}`, uuid.Nil)
	if w.Code != http.StatusOK {
		t.Fatalf("the change was made, so the answer is 200: %d %s", w.Code, w.Body)
	}
	var info AccountInfo
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.Email != "ada@example.org" || info.EmailVerified || info.ConfirmationSent == nil || *info.ConfirmationSent {
		t.Fatalf("%+v", info)
	}
}

func TestSpentMailBudgetDoesNotTurnASavedAddressIntoAnError(t *testing.T) {
	pool, _, member := authFixture(t)
	h := NewHandler(&Sessions{DB: pool, Secret: "export-test-secret-export-test-secret"}, nil)
	h.Mail = okMail{}
	for i := 0; i < tokensPerHour; i++ {
		if _, err := issueToken(context.Background(), pool, member.ID, purposeEmailVerify, "old@example.org", EmailVerifyTTL); err != nil {
			t.Fatal(err)
		}
	}
	w := authRequest(t, h.setEmail, member, `{"email":"a6@example.org","current_password":"`+testPassword+`"}`, uuid.Nil)
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var last AccountInfo
	if err := json.Unmarshal(w.Body.Bytes(), &last); err != nil || last.ConfirmationSent == nil || *last.ConfirmationSent {
		t.Fatalf("the sixth mail is over the hourly budget: %+v, %v", last, err)
	}
	var stored string
	if err := pool.QueryRow(context.Background(), `SELECT email FROM users WHERE id = $1`, member.ID).Scan(&stored); err != nil || stored != "a6@example.org" {
		t.Fatalf("stored %q, %v", stored, err)
	}
}

type okMail struct{}

func (okMail) Send(context.Context, mailer.Message) error { return nil }
