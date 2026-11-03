//go:build integration

package webhook

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/textproto"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/testutil/pgfault"
)

func TestThreadLookupFailureIsNotAnUnknownChannel(t *testing.T) {
	f := newFixture(t)
	rec, token := f.newHook("hook")
	path := "/webhooks/" + rec.ID.String() + "/" + token + "?thread_id="

	wantStatus(t, f.json(f.h, "POST", path+uuid.NewString(), `{"content":"x"}`), 404, "missing thread")
	h := f.faulty(&pgfault.Tracer{Query: "SELECT channel_id, user_id, parent_id FROM messages"})
	wantStatus(t, f.json(h, "POST", path+uuid.NewString(), `{"content":"x"}`), 500, "thread lookup failure")
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

func TestLateFailureDoesNotSwitchOffAnAdminDisabledWebhook(t *testing.T) {
	f := newFixture(t)
	hook := f.outgoing("https://hooks.example/x")
	ctx := context.Background()
	fail := Delivery{EventID: uuid.New(), EventType: EventMessageCreate, Attempt: 1}
	if _, err := f.p.Exec(ctx, `UPDATE outgoing_webhooks SET consecutive_failures = $2, enabled = FALSE WHERE id = $1`, hook.ID, disableAfter-1); err != nil {
		t.Fatal(err)
	}
	off, err := recordDelivery(ctx, f.p, hook.ID, fail, true, true)
	if err != nil || off {
		t.Fatalf("admin-disabled webhook: switchedOff=%v err=%v", off, err)
	}
	var auto bool
	if err := f.p.QueryRow(ctx, `SELECT auto_disabled FROM outgoing_webhooks WHERE id = $1`, hook.ID).Scan(&auto); err != nil || auto {
		t.Fatalf("auto_disabled=%v err=%v", auto, err)
	}

	// An enabled one at the threshold switches off exactly once.
	if _, err := f.p.Exec(ctx, `UPDATE outgoing_webhooks SET enabled = TRUE WHERE id = $1`, hook.ID); err != nil {
		t.Fatal(err)
	}
	if off, err = recordDelivery(ctx, f.p, hook.ID, fail, true, true); err != nil || !off {
		t.Fatalf("first failure at the threshold: switchedOff=%v err=%v", off, err)
	}
	if off, err = recordDelivery(ctx, f.p, hook.ID, fail, true, true); err != nil || off {
		t.Fatalf("late failure of an auto-disabled webhook: switchedOff=%v err=%v", off, err)
	}
}
