//go:build integration

package webhook

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/media"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/ssrf"
	"github.com/msch128/mnema-talk/internal/testutil"
	"github.com/msch128/mnema-talk/internal/testutil/pgfault"
)

func TestMain(m *testing.M) { os.Exit(testutil.Main(m)) }

var png = []byte{
	0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0x0d, 'I', 'H', 'D', 'R', 0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0,
	0x1f, 0x15, 0xc4, 0x89, 0, 0, 0, 0x0d, 'I', 'D', 'A', 'T', 0x78, 0x9c, 0x63, 0xf8, 0xcf, 0xc0, 0xf0, 0x1f, 0, 5, 0, 1, 0xff,
	0x89, 0x99, 0x3d, 0x1d, 0, 0, 0, 0, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
}

type fixture struct {
	t     *testing.T
	p     *db.Pool
	store *media.MemoryStore
	h     *Handler
	owner *auth.User
	ch    uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	p := testutil.DB(t)
	testutil.Reset(t, p)
	f := &fixture{t: t, p: p, store: media.NewMemoryStore()}
	var owner uuid.UUID
	if err := p.QueryRow(context.Background(), `
		INSERT INTO users (username, display_name, password_hash, role) VALUES ('owner', 'owner', 'x', 'admin') RETURNING id`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	f.owner = &auth.User{ID: owner, Role: "admin"}
	if err := p.QueryRow(context.Background(), `INSERT INTO channels (name, type) VALUES ('c', 'text') RETURNING id`).Scan(&f.ch); err != nil {
		t.Fatal(err)
	}
	f.h = f.handler(p)
	return f
}

// handler builds a Handler (no permission model) on pool.
func (f *fixture) handler(p *db.Pool) *Handler {
	m := &media.Handler{DB: p, Store: f.store, Bucket: "b", MaxUploadBytes: 1 << 20, Events: &events.Recorder{}}
	policy := &ssrf.Policy{}
	h := New(Handler{
		DB: p, Events: &events.Recorder{}, Media: m, PublicURL: "http://example.test/", Policy: policy,
		Outgoing: NewDispatcher(p, ssrf.NewClient(ssrf.NewDialer(policy), time.Second), "test"),
	})
	h.SetLimits(10000, 10000)
	return h
}

// faulty is a handler whose pool cancels the first statement containing query.
func (f *fixture) faulty(tr *pgfault.Tracer) *Handler {
	pool := &db.Pool{Pool: pgfault.Pool(f.t, f.p.Pool, tr)}
	return f.handler(pool)
}

func (f *fixture) serve(h *Handler, method, path string, body io.Reader, hdr map[string]string) *httptest.ResponseRecorder {
	f.t.Helper()
	r := chi.NewRouter()
	h.MountPublic(r)
	r.Route("/admin", func(a chi.Router) {
		a.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), f.owner)))
			})
		})
		h.MountAdmin(a)
		h.MountAdminUploads(a)
	})
	req := httptest.NewRequest(method, path, body)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func (f *fixture) json(h *Handler, method, path, body string) *httptest.ResponseRecorder {
	return f.serve(h, method, path, strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
}

func (f *fixture) newHook(name string) (*record, string) {
	f.t.Helper()
	rec, token, err := create(context.Background(), f.p, f.ch, f.owner.ID, name, DefaultSettings)
	if err != nil {
		f.t.Fatal(err)
	}
	return rec, token
}

func wantStatus(t *testing.T, w *httptest.ResponseRecorder, status int, what string) {
	t.Helper()
	if w.Code != status {
		t.Errorf("%s: status %d, want %d: %s", what, w.Code, status, w.Body)
	}
}

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

// A permission model that cannot load fails closed on every route.
func TestFailsClosedWithoutPermissionState(t *testing.T) {
	f := newFixture(t)
	rec, _ := f.newHook("hook")
	f.h.Perms = perm.NewService(nil)
	admin := "/admin/webhooks/" + rec.ID.String()
	f.owner = &auth.User{ID: uuid.New(), Role: "user"}
	for name, w := range map[string]*httptest.ResponseRecorder{
		"list":       f.serve(f.h, "GET", "/admin/channels/"+f.ch.String()+"/webhooks", nil, nil),
		"create":     f.json(f.h, "POST", "/admin/channels/"+f.ch.String()+"/webhooks", `{"name":"n"}`),
		"rename":     f.json(f.h, "PATCH", admin, `{"name":"n"}`),
		"regenerate": f.serve(f.h, "POST", admin+"/token", nil, nil),
		"delete":     f.serve(f.h, "DELETE", admin, nil, nil),
	} {
		wantStatus(t, w, 500, name)
	}
	// Executing needs the creator's permissions for @everyone only; the
	// default policy asks the model to filter who can see the channel.
	w := f.json(f.h, "POST", "/webhooks/"+rec.ID.String()+"/"+"x", `{"content":"hi"}`)
	wantStatus(t, w, 401, "wrong token")
}

func TestExecuteFailures(t *testing.T) {
	f := newFixture(t)
	rec, token := f.newHook("hook")
	path := "/webhooks/" + rec.ID.String() + "/" + token

	// The webhook cannot be loaded.
	h := f.faulty(&pgfault.Tracer{Query: "w.id = $1"})
	wantStatus(t, f.json(h, "POST", path, `{"content":"x"}`), 500, "load")
	// The insert fails; so does reading the message back.
	h = f.faulty(&pgfault.Tracer{Query: "INSERT INTO messages"})
	wantStatus(t, f.json(h, "POST", path, `{"content":"x"}`), 500, "insert")
	h = f.faulty(&pgfault.Tracer{Query: "m.id = $1"})
	wantStatus(t, f.json(h, "POST", path, `{"content":"x"}`), 500, "read back")

	// A failed insert after a stored file removes the file again.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("payload_json", `{"content":"x"}`)
	hd := textproto.MIMEHeader{}
	hd.Set("Content-Disposition", `form-data; name="files[0]"; filename="a.png"`)
	hd.Set("Content-Type", "image/png")
	part, _ := mw.CreatePart(hd)
	_, _ = part.Write(png)
	_ = mw.Close()
	h = f.faulty(&pgfault.Tracer{Query: "INSERT INTO messages"})
	wantStatus(t, f.serve(h, "POST", path, bytes.NewReader(buf.Bytes()), map[string]string{"Content-Type": mw.FormDataContentType()}), 500, "insert with file")
	if f.store.Len() != 0 {
		t.Fatalf("file left behind: %d", f.store.Len())
	}

	// Storage is not configured: files are refused, plain posts work.
	noStore := f.handler(f.p)
	noStore.Media.Store = nil
	wantStatus(t, f.serve(noStore, "POST", path, bytes.NewReader(buf.Bytes()), map[string]string{"Content-Type": mw.FormDataContentType()}), 503, "file without storage")
	wantStatus(t, f.json(noStore, "POST", path, `{"content":"plain"}`), 204, "plain without storage")
}

func TestExecuteBodyEdges(t *testing.T) {
	f := newFixture(t)
	rec, token := f.newHook("hook")
	path := "/webhooks/" + rec.ID.String() + "/" + token
	do := func(ct, body string) *httptest.ResponseRecorder {
		return f.serve(f.h, "POST", path, strings.NewReader(body), map[string]string{"Content-Type": ct})
	}
	wantStatus(t, do("application/json", `{"content":"a"} {"content":"b"}`), 400, "trailing data")
	wantStatus(t, do("application/json", `[1]`), 400, "array")
	wantStatus(t, do("", `{"content":"a"}`), 400, "no content type")
	wantStatus(t, do("text/plain", `x`), 400, "text")
	wantStatus(t, do("multipart/form-data", `x`), 400, "multipart without boundary")
	wantStatus(t, do("multipart/form-data; boundary=zz", "--zz\r\nContent-Disposition: form-data; name=\"content\"\r\n\r\nhi"), 400, "truncated multipart")
	wantStatus(t, do("multipart/form-data; boundary=zz", "--zz\r\nContent-Disposition: form-data; name=\"payload_json\"\r\n\r\n{\"content\""), 400, "truncated payload_json")
	wantStatus(t, do("multipart/form-data; boundary=zz", "--zz\r\nContent-Disposition: form-data; name=\"payload_json\"\r\n\r\n"+strings.Repeat("x", 1<<20+10)+"\r\n--zz--\r\n"), 413, "payload_json too large")
	wantStatus(t, do("multipart/form-data; boundary=zz", "--zz\r\nContent-Disposition: form-data; name=\"payload_json\"\r\n\r\nnot json\r\n--zz--\r\n"), 400, "payload_json invalid")
	wantStatus(t, do("multipart/form-data; boundary=zz", "--zz\r\nContent-Disposition: form-data; name=\"content\"\r\n\r\n"+strings.Repeat("x", 20000)+"\r\n--zz--\r\n"), 400, "content too long")
	wantStatus(t, do("multipart/form-data; boundary=zz", "--zz\r\nContent-Disposition: form-data; name=\"other\"\r\n\r\nignored\r\n--zz--\r\n"), 400, "nothing to post")

	// A truncated file leaves nothing in storage.
	trunc := "--zz\r\nContent-Disposition: form-data; name=\"files[0]\"; filename=\"a.png\"\r\nContent-Type: image/png\r\n\r\n" + string(png)
	wantStatus(t, do("multipart/form-data; boundary=zz", trunc), 400, "truncated file")
	if f.store.Len() != 0 {
		t.Fatalf("file left behind: %d", f.store.Len())
	}
	// A bad part after a good file removes the file.
	two := "--zz\r\nContent-Disposition: form-data; name=\"files[0]\"; filename=\"a.png\"\r\nContent-Type: image/png\r\n\r\n" + string(png) + "\r\n--zz\r\nContent-Disposition: form-data; name=\"payload_json\"\r\n\r\n" + strings.Repeat("x", 1<<20+10) + "\r\n--zz--\r\n"
	wantStatus(t, do("multipart/form-data; boundary=zz", two), 413, "payload_json after file")
	if f.store.Len() != 0 {
		t.Fatalf("file left behind: %d", f.store.Len())
	}
	// Whole-request cap.
	f.h.Media.MaxUploadBytes = 1 << 10
	big := "--zz\r\nContent-Disposition: form-data; name=\"files[0]\"; filename=\"a.png\"\r\nContent-Type: image/png\r\n\r\n" + string(png) + strings.Repeat("0", 3<<20) + "\r\n--zz--\r\n"
	wantStatus(t, do("multipart/form-data; boundary=zz", big), 413, "request too large")
	if f.store.Len() != 0 {
		t.Fatalf("file left behind: %d", f.store.Len())
	}
}

func TestExecuteWithPermissionModel(t *testing.T) {
	f := newFixture(t)
	f.h.Perms = perm.NewService(f.p)
	rec, token := f.newHook("hook")
	path := "/webhooks/" + rec.ID.String() + "/" + token
	wantStatus(t, f.json(f.h, "POST", path, `{"content":"fine"}`), 204, "webhook")
	// The webhook stands on its own: the account that made it does not matter.
	if _, err := f.p.Exec(context.Background(), `UPDATE users SET disabled_at = NOW() WHERE id = $1`, f.owner.ID); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, f.json(f.h, "POST", path, `{"content":"still fine"}`), 204, "maker disabled")
	if _, err := f.p.Exec(context.Background(), `UPDATE webhooks SET created_by = NULL`); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, f.json(f.h, "POST", path, `{"content":"still fine"}`), 204, "maker gone")
	// An unreadable permission model refuses the post instead of guessing.
	f.h.Perms = perm.NewService(nil)
	wantStatus(t, f.json(f.h, "POST", path, `{"content":"hi"}`), 500, "no permission state")
}

// Each post is judged by the webhook's own settings.
func TestExecuteAppliesTheWebhooksSettings(t *testing.T) {
	f := newFixture(t)
	rec, token := f.newHook("Hook")
	path := "/webhooks/" + rec.ID.String() + "/" + token + "?wait=true"
	var other uuid.UUID
	if err := f.p.QueryRow(context.Background(), `INSERT INTO users (username, display_name, password_hash) VALUES ('bob', 'bob', 'x') RETURNING id`).Scan(&other); err != nil {
		t.Fatal(err)
	}
	post := func(body string) *httptest.ResponseRecorder { return f.json(f.h, "POST", path, body) }
	set := func(patch string) {
		t.Helper()
		wantStatus(t, f.json(f.h, "PATCH", "/admin/webhooks/"+rec.ID.String(), patch), 200, "patch "+patch)
	}
	mentions := func(body string) int {
		t.Helper()
		w := post(body)
		wantStatus(t, w, 200, body)
		return strings.Count(w.Body.String(), other.String())
	}

	// Defaults: members can be mentioned, @all not, even when asked for.
	if mentions(`{"content":"@bob"}`) != 1 || mentions(`{"content":"@all","allowed_mentions":{"parse":["everyone"]}}`) != 0 {
		t.Fatal("default mention rules")
	}
	// Everyone-mentions on: the sender still has to ask.
	set(`{"allow_everyone":true}`)
	if mentions(`{"content":"@all"}`) != 0 || mentions(`{"content":"@all","allowed_mentions":{"parse":["everyone"]}}`) != 1 {
		t.Fatal("allow_everyone rules")
	}
	// User mentions off: not even by an explicit list.
	set(`{"allow_user_mentions":false,"allow_everyone":false}`)
	if mentions(`{"content":"@bob","allowed_mentions":{"parse":["users"]}}`) != 0 || mentions(`{"content":"@bob","allowed_mentions":{"users":["`+other.String()+`"]}}`) != 0 {
		t.Fatal("allow_user_mentions off")
	}
	set(`{"allow_user_mentions":true}`)

	// Identity override.
	if w := post(`{"content":"x","username":"Custom"}`); !strings.Contains(w.Body.String(), `"display_name":"Custom"`) {
		t.Fatalf("override allowed: %s", w.Body)
	}
	set(`{"allow_identity_override":false}`)
	if w := post(`{"content":"x","username":"Custom"}`); !strings.Contains(w.Body.String(), `"display_name":"Hook"`) {
		t.Fatalf("override ignored: %s", w.Body)
	}

	// Embeds and attachments.
	embed := `{"content":"x","embeds":[{"title":"t"}]}`
	wantStatus(t, post(embed), 200, "embeds on")
	set(`{"allow_embeds":false}`)
	wantStatus(t, post(embed), 400, "embeds off")
	wantStatus(t, post(`{"content":"plain"}`), 200, "plain with embeds off")
	form, ct := avatarForm()
	_ = form
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("payload_json", `{"content":"x"}`)
	hd := textproto.MIMEHeader{}
	hd.Set("Content-Disposition", `form-data; name="files[0]"; filename="a.png"`)
	hd.Set("Content-Type", "image/png")
	part, _ := mw.CreatePart(hd)
	_, _ = part.Write(png)
	_ = mw.Close()
	_ = ct
	file := func() *httptest.ResponseRecorder {
		return f.serve(f.h, "POST", path, bytes.NewReader(buf.Bytes()), map[string]string{"Content-Type": mw.FormDataContentType()})
	}
	wantStatus(t, file(), 200, "attachments on")
	set(`{"allow_attachments":false}`)
	before := f.store.Len()
	wantStatus(t, file(), 400, "attachments off")
	if f.store.Len() != before {
		t.Fatal("a refused file was kept")
	}
	// Settings can be set when creating, and unknown ones are refused.
	w := f.json(f.h, "POST", "/admin/channels/"+f.ch.String()+"/webhooks", `{"name":"quiet","allow_embeds":false,"allow_everyone":true}`)
	wantStatus(t, w, 201, "create with settings")
	if !strings.Contains(w.Body.String(), `"allow_embeds":false`) || !strings.Contains(w.Body.String(), `"allow_everyone":true`) || !strings.Contains(w.Body.String(), `"allow_attachments":true`) {
		t.Fatalf("created settings: %s", w.Body)
	}
	wantStatus(t, f.json(f.h, "POST", "/admin/channels/"+f.ch.String()+"/webhooks", `{"name":"x","allow_nothing":true}`), 400, "unknown setting")
	// Regenerating the token touches nothing else.
	wantStatus(t, f.serve(f.h, "POST", "/admin/webhooks/"+rec.ID.String()+"/token", nil, nil), 200, "regenerate")
	cur, _ := get(context.Background(), f.p, rec.ID)
	if cur.AllowEmbeds || cur.AllowAttachments || cur.AllowIdentityOverride {
		t.Fatalf("regenerating changed the settings: %+v", cur.Settings)
	}
}
