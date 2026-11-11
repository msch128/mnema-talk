//go:build integration

package webhook

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
	"github.com/msch128/mnema-talk/internal/testutil/pgfault"
)

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
	// The message is stored by then: the sender gets 204, not an error that
	// would make it post a duplicate.
	h = f.faulty(&pgfault.Tracer{Query: "m.id = $1"})
	wantStatus(t, f.json(h, "POST", path, `{"content":"read back"}`), 204, "read back")
	var stored int
	if err := f.p.QueryRow(context.Background(), `SELECT COUNT(*) FROM messages WHERE content = 'read back'`).Scan(&stored); err != nil || stored != 1 {
		t.Fatalf("stored messages %d, err %v", stored, err)
	}

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
	f.h.Perms = permtest.Unloadable()
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
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("payload_json", `{"content":"x"}`)
	hd := textproto.MIMEHeader{}
	hd.Set("Content-Disposition", `form-data; name="files[0]"; filename="a.png"`)
	hd.Set("Content-Type", "image/png")
	part, _ := mw.CreatePart(hd)
	_, _ = part.Write(png)
	_ = mw.Close()
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

func TestExecuteMultipartEdges(t *testing.T) {
	f := newFixture(t)
	rec, token := f.newHook("hook")
	path := "/webhooks/" + rec.ID.String() + "/" + token
	// A broken part header ends the form.
	w := f.serve(f.h, "POST", path, strings.NewReader("--zz\r\nnot a header\r\n\r\nx\r\n--zz--\r\n"), map[string]string{"Content-Type": "multipart/form-data; boundary=zz"})
	wantStatus(t, w, 400, "broken part header")

	// The media row cannot be written: the stored file goes again.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("payload_json", `{"content":"x"}`)
	hd := textproto.MIMEHeader{}
	hd.Set("Content-Disposition", `form-data; name="files[0]"; filename="a.png"`)
	hd.Set("Content-Type", "image/png")
	part, _ := mw.CreatePart(hd)
	_, _ = part.Write(png)
	_ = mw.Close()
	h := f.faulty(&pgfault.Tracer{Query: "INSERT INTO media"})
	wantStatus(t, f.serve(h, "POST", path, bytes.NewReader(buf.Bytes()), map[string]string{"Content-Type": mw.FormDataContentType()}), 500, "record file")
	if f.store.Len() != 0 {
		t.Fatalf("file left behind: %d", f.store.Len())
	}
}

func TestThreadLookupFailureIsNotAnUnknownChannel(t *testing.T) {
	f := newFixture(t)
	rec, token := f.newHook("hook")
	path := "/webhooks/" + rec.ID.String() + "/" + token + "?thread_id="

	wantStatus(t, f.json(f.h, "POST", path+uuid.NewString(), `{"content":"x"}`), 404, "missing thread")
	h := f.faulty(&pgfault.Tracer{Query: "SELECT channel_id, user_id, parent_id FROM messages"})
	wantStatus(t, f.json(h, "POST", path+uuid.NewString(), `{"content":"x"}`), 500, "thread lookup failure")
}
