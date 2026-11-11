//go:build integration

package webhook

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"sync"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/media"
	"github.com/msch128/mnema-talk/internal/testutil/pgfault"
)

// Every database statement of the management API can fail; the answer is a
// sanitized 500 and nothing half-done is left behind.
func TestManagementDatabaseFailures(t *testing.T) {
	f := newFixture(t)
	rec, _ := f.newHook("hook")
	admin := "/admin/webhooks/" + rec.ID.String()
	list := "/admin/channels/" + f.ch.String() + "/webhooks"

	cases := []struct {
		name           string
		query          string
		afterPrepare   bool
		method, path   string
		body           string
		wantStatusCode int
	}{
		{"list query", "w.channel_id = $1", false, "GET", list, "", 500},
		{"list stream", "w.channel_id = $1", true, "GET", list, "", 500},
		{"create count", "SELECT COUNT(*) FROM webhooks", false, "POST", list, `{"name":"n"}`, 500},
		{"create bot", "INSERT INTO users", false, "POST", list, `{"name":"n"}`, 500},
		{"create webhook", "INSERT INTO webhooks", false, "POST", list, `{"name":"n"}`, 500},
		{"rename load", "w.id = $1", false, "PATCH", admin, `{"name":"n"}`, 500},
		{"rename webhook", "UPDATE webhooks SET name", false, "PATCH", admin, `{"name":"n"}`, 500},
		{"rename bot", "UPDATE users SET display_name", false, "PATCH", admin, `{"name":"n"}`, 500},
		{"regenerate", "UPDATE webhooks SET token_hash", false, "POST", admin + "/token", "", 500},
		{"delete check", "SELECT EXISTS (SELECT 1 FROM messages", false, "DELETE", admin, "", 500},
		{"delete webhook", "DELETE FROM webhooks", false, "DELETE", admin, "", 500},
		{"delete bot", "DELETE FROM users", false, "DELETE", admin, "", 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := f.faulty(&pgfault.Tracer{Query: tc.query, AfterPrepare: tc.afterPrepare})
			var w *httptest.ResponseRecorder
			if tc.body == "" {
				w = f.serve(h, tc.method, tc.path, nil, nil)
			} else {
				w = f.json(h, tc.method, tc.path, tc.body)
			}
			wantStatus(t, w, tc.wantStatusCode, tc.name)
			if strings.Contains(w.Body.String(), "cancel") {
				t.Errorf("database error leaked: %s", w.Body)
			}
		})
	}
	// The fault in delete left the webhook alone; nothing changed.
	if _, err := get(context.Background(), f.p, rec.ID); err != nil {
		t.Fatalf("webhook damaged by failed operations: %v", err)
	}
}

// The create answer is read back after the insert; losing that read is a 500
// too (the webhook exists, the secret was not shown).
func TestCreateReadBackFailure(t *testing.T) {
	f := newFixture(t)
	h := f.faulty(&pgfault.Tracer{Query: "w.id = $1"})
	wantStatus(t, f.json(h, "POST", "/admin/channels/"+f.ch.String()+"/webhooks", `{"name":"n"}`), 500, "create read-back")
}

func TestAvatarAndDeleteEdges(t *testing.T) {
	f := newFixture(t)
	rec, _ := f.newHook("hook")
	admin := "/admin/webhooks/" + rec.ID.String()

	// No storage: 503. Bad form: 400. Then a real one.
	noStore := f.handler(f.p)
	noStore.Media.Store = nil
	wantStatus(t, f.serve(noStore, "POST", admin+"/avatar", strings.NewReader("x"), nil), 503, "avatar without storage")
	wantStatus(t, f.serve(f.h, "POST", admin+"/avatar", strings.NewReader("x"), nil), 400, "avatar without form")

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	hd := textproto.MIMEHeader{}
	hd.Set("Content-Disposition", `form-data; name="avatar"; filename="a.png"`)
	hd.Set("Content-Type", "image/png")
	part, _ := mw.CreatePart(hd)
	_, _ = part.Write(png)
	_ = mw.Close()
	body := buf.Bytes()
	wantStatus(t, f.serve(f.h, "POST", admin+"/avatar", bytes.NewReader(body), map[string]string{"Content-Type": mw.FormDataContentType()}), 200, "avatar")

	// Unknown id and unreadable id.
	wantStatus(t, f.json(f.h, "PATCH", "/admin/webhooks/"+uuid.NewString(), `{"name":"x"}`), 404, "unknown")
	wantStatus(t, f.json(f.h, "PATCH", "/admin/webhooks/zzz", `{"name":"x"}`), 400, "bad id")
	wantStatus(t, f.json(f.h, "PATCH", admin, `{"name":""}`), 400, "empty name")
	wantStatus(t, f.json(f.h, "PATCH", admin, `{"nope":1}`), 400, "unknown field")
	wantStatus(t, f.json(f.h, "POST", "/admin/channels/zzz/webhooks", `{"name":"x"}`), 400, "bad channel id")
	wantStatus(t, f.json(f.h, "GET", "/admin/channels/zzz/webhooks", ``), 400, "bad channel id list")
	wantStatus(t, f.json(f.h, "POST", "/admin/channels/"+f.ch.String()+"/webhooks", `{"nope":1}`), 400, "create unknown field")
	wantStatus(t, f.json(f.h, "POST", "/admin/channels/"+f.ch.String()+"/webhooks", `{"name":""}`), 400, "create empty")

	// Deleting a webhook whose avatar object cannot be removed still works.
	broken := f.handler(f.p)
	broken.Media.Store = failingStore{broken.Media.Store}
	wantStatus(t, f.serve(broken, "DELETE", admin, nil, nil), 204, "delete with failing store")
	if f.store.Len() != 1 {
		t.Fatalf("avatar object count %d", f.store.Len())
	}
}

// failingStore refuses deletes.
type failingStore struct{ media.Store }

func (failingStore) Delete(context.Context, string) error { return errors.New("store down") }

func avatarForm() ([]byte, string) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	hd := textproto.MIMEHeader{}
	hd.Set("Content-Disposition", `form-data; name="avatar"; filename="a.png"`)
	hd.Set("Content-Type", "image/png")
	part, _ := mw.CreatePart(hd)
	_, _ = part.Write(png)
	_ = mw.Close()
	return buf.Bytes(), mw.FormDataContentType()
}

// The answer of a rename or avatar change is read back after the change.
func TestResponseReadBackFailures(t *testing.T) {
	f := newFixture(t)
	rec, _ := f.newHook("hook")
	admin := "/admin/webhooks/" + rec.ID.String()
	h := f.failing(failingRecords{failLoad: 2})
	wantStatus(t, f.json(h, "PATCH", admin, `{"name":"n"}`), 500, "rename read-back")

	form, ct := avatarForm()
	h = f.failing(failingRecords{failLoad: 2})
	wantStatus(t, f.serve(h, "POST", admin+"/avatar", bytes.NewReader(form), map[string]string{"Content-Type": ct}), 500, "avatar read-back")
	h = f.failing(failingRecords{failLoad: 1})
	wantStatus(t, f.serve(h, "POST", admin+"/avatar", bytes.NewReader(form), map[string]string{"Content-Type": ct}), 500, "avatar load")
	h = f.failing(failingRecords{failLoad: 1})
	wantStatus(t, f.serve(h, "POST", admin+"/token", nil, nil), 500, "token load")
}

func TestConcurrentCreatesRespectTheChannelCap(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for range maxPerChannel + 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = create(ctx, f.p, f.ch, f.owner.ID, "hook", DefaultSettings)
		}()
	}
	for range maxOutgoingPerChannel + 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = createOutgoing(ctx, f.p, f.ch, f.owner.ID, "out", "https://hooks.example/x", []string{EventMessageCreate})
		}()
	}
	wg.Wait()
	for table, want := range map[string]int{"webhooks": maxPerChannel, "outgoing_webhooks": maxOutgoingPerChannel} {
		var n int
		if err := f.p.QueryRow(ctx, `SELECT COUNT(*) FROM `+table).Scan(&n); err != nil || n != want {
			t.Errorf("%s: %d rows (err %v), want %d", table, n, err, want)
		}
	}
}

func (f *fixture) setAvatar(rec *record) *record {
	f.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	hd := textproto.MIMEHeader{}
	hd.Set("Content-Disposition", `form-data; name="avatar"; filename="a.png"`)
	hd.Set("Content-Type", "image/png")
	part, _ := mw.CreatePart(hd)
	_, _ = part.Write(png)
	_ = mw.Close()
	w := f.serve(f.h, "POST", "/admin/webhooks/"+rec.ID.String()+"/avatar", bytes.NewReader(buf.Bytes()), map[string]string{"Content-Type": mw.FormDataContentType()})
	wantStatus(f.t, w, 200, "avatar")
	out, err := get(context.Background(), f.p, rec.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

func TestRemoveKeepsTheAvatarWhenTheRecordsStay(t *testing.T) {
	f := newFixture(t)
	rec, _ := f.newHook("hook")
	rec = f.setAvatar(rec)
	ctx := context.Background()

	// A failing delete leaves webhook, account and avatar object alone.
	h := f.faulty(&pgfault.Tracer{Query: "DELETE FROM users WHERE id = $1"})
	wantStatus(t, f.serve(h, "DELETE", "/admin/webhooks/"+rec.ID.String(), nil, nil), 500, "failing delete")
	if f.store.Len() != 1 {
		t.Fatalf("avatar objects after a failed delete: %d", f.store.Len())
	}
	if _, err := get(ctx, f.p, rec.ID); err != nil {
		t.Fatalf("webhook gone after a failed delete: %v", err)
	}

	// Once it has posted, the account stays for the history and so does the avatar.
	if _, err := f.p.Exec(ctx, `INSERT INTO messages (channel_id, user_id, content) VALUES ($1, $2, 'hi')`, f.ch, rec.botUserID); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, f.serve(f.h, "DELETE", "/admin/webhooks/"+rec.ID.String(), nil, nil), 204, "delete with history")
	if f.store.Len() != 1 {
		t.Fatalf("avatar objects with history: %d", f.store.Len())
	}
}

func TestRemoveDeletesTheAvatarOfAGoneAccount(t *testing.T) {
	f := newFixture(t)
	rec, _ := f.newHook("hook")
	rec = f.setAvatar(rec)
	wantStatus(t, f.serve(f.h, "DELETE", "/admin/webhooks/"+rec.ID.String(), nil, nil), 204, "delete")
	if f.store.Len() != 0 {
		t.Fatalf("avatar object left behind: %d", f.store.Len())
	}
}
