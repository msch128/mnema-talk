//go:build integration

package discordimport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
)

type voiceSpy struct{ closed []uuid.UUID }

func (v *voiceSpy) CloseVoiceChannel(id uuid.UUID) { v.closed = append(v.closed, id) }

type objectSpy struct{ keys []string }

func (o *objectSpy) DeleteBatch(_ context.Context, keys []string) error {
	o.keys = append(o.keys, keys...)
	return nil
}

func eventTypes(rec *events.Recorder) []string {
	var out []string
	for _, e := range rec.Snapshot() {
		out = append(out, e.Type)
	}
	return out
}

func TestApplyAndUndoTellClientsAndCloseTheRemovedRooms(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	rec := &events.Recorder{}
	voice := &voiceSpy{}
	w.im.Events, w.im.Voice = rec, voice

	imp, err := w.im.Apply(ctx, w.admin, "adm", applyReq(w.preview(w.admin)))
	w.must(err)
	if got := eventTypes(rec); !slices.Contains(got, events.ChannelsChanged) || !slices.Contains(got, events.RolesChanged) {
		t.Fatalf("events after apply = %v", got)
	}

	before := len(rec.Snapshot())
	_, err = w.im.Undo(ctx, w.admin, imp.ID, UndoRequest{})
	w.must(err)
	if len(rec.Snapshot()) <= before {
		t.Fatal("undo told nobody that channels and roles changed")
	}
	if len(voice.closed) != imp.Counts.Channels {
		t.Fatalf("closed %d rooms, want one per removed channel (%d)", len(voice.closed), imp.Counts.Channels)
	}
}

func TestUndoDeletesTheMediaOfRemovedChannels(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	objects := &objectSpy{}
	w.im.Objects = objects
	imp, err := w.im.Apply(ctx, w.admin, "adm", applyReq(w.preview(w.admin)))
	w.must(err)
	_, err = w.p.Exec(ctx, `
		WITH m AS (INSERT INTO messages (channel_id, user_id, content) SELECT c.id, $1, 'pic' FROM channels c WHERE c.name = 'rules' RETURNING id)
		INSERT INTO media (uploader_id, message_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes) SELECT $1, id, 'b', 'k/pic.png', 'pic.png', 'image/png', 3 FROM m`, w.admin.ID)
	w.must(err)

	_, err = w.im.Undo(ctx, w.admin, imp.ID, UndoRequest{AcceptMessageLoss: true})
	w.must(err)
	if !slices.Contains(objects.keys, "k/pic.png") {
		t.Fatalf("deleted objects = %v", objects.keys)
	}
}

func TestApplyRefusesWhatCannotBeImported(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	plan := w.preview(w.admin)

	unconfirmed := applyReq(plan)
	unconfirmed.ConfirmRoles = false
	badLink := applyReq(plan)
	badLink.Template = "https://evil.example/template/AbCdEf123456"
	for name, c := range map[string]struct {
		req    ApplyRequest
		status int
	}{
		"unconfirmed":  {unconfirmed, http.StatusBadRequest},
		"foreign link": {badLink, http.StatusBadRequest},
	} {
		_, err := w.im.Apply(ctx, w.admin, "adm", c.req)
		wantStatus(t, name, err, c.status)
	}

	_, err := w.im.Preview(ctx, w.admin, "not a link")
	wantStatus(t, "preview of a non-link", err, http.StatusBadRequest)
}

func TestTemplatesDiscordDoesNotKnowOrCannotServeAreRefused(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	empty := `{"code":"AbCdEf123456","name":"x","serialized_source_guild":{"name":"x","roles":[],"channels":[]}}`
	for _, c := range []struct {
		name         string
		served, want int
		body         string
	}{
		{"not shared", http.StatusNotFound, http.StatusNotFound, ""},
		{"discord down", http.StatusBadGateway, http.StatusServiceUnavailable, ""},
		{"garbage", http.StatusOK, http.StatusBadRequest, "<html>"},
		{"nothing in it", http.StatusOK, http.StatusBadRequest, empty},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			rw.WriteHeader(c.served)
			_, _ = rw.Write([]byte(c.body))
		}))
		w.im.Fetcher = loopbackFetcher(srv.URL)
		_, perr := w.im.Preview(ctx, w.admin, "AbCdEf123456")
		_, aerr := w.im.Apply(ctx, w.admin, "adm", ApplyRequest{Template: "AbCdEf123456", ConfirmAdditive: true, ConfirmRoles: true})
		srv.Close()
		wantStatus(t, c.name+" (preview)", perr, c.want)
		wantStatus(t, c.name+" (apply)", aerr, c.want)
	}
	if n := w.tables()["discord_imports"]; n != 0 {
		t.Fatalf("%d imports recorded", n)
	}
}

func TestEverythingFailsCleanlyWhenTheRequestIsCanceled(t *testing.T) {
	w := newWorld(t)
	imp, err := w.im.Apply(context.Background(), w.admin, "adm", applyReq(w.preview(w.admin)))
	w.must(err)
	before := w.tables()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := applyReq(&Plan{})
	calls := map[string]func() error{
		"preview": func() error { _, err := w.im.Preview(ctx, w.admin, "AbCdEf123456"); return err },
		"apply":   func() error { _, err := w.im.Apply(ctx, w.admin, "adm", req); return err },
		"list":    func() error { _, err := w.im.List(ctx); return err },
		"detail":  func() error { _, err := w.im.Detail(ctx, imp.ID); return err },
		"undo":    func() error { _, err := w.im.Undo(ctx, w.admin, imp.ID, UndoRequest{}); return err },
	}
	for name, call := range calls {
		if call() == nil {
			t.Errorf("%s succeeded on a canceled request", name)
		}
	}
	if after := w.tables(); len(after) != len(before) || after["channels"] != before["channels"] || after["roles"] != before["roles"] {
		t.Fatalf("a canceled request changed data: %v -> %v", before, after)
	}
}

func TestDetailOfAnUndoneImportHasNothingLeft(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	imp, err := w.im.Apply(ctx, w.admin, "adm", applyReq(w.preview(w.admin)))
	w.must(err)
	_, err = w.im.Undo(ctx, w.admin, imp.ID, UndoRequest{})
	w.must(err)

	d, err := w.im.Detail(ctx, imp.ID)
	w.must(err)
	if d.UndoneAt == nil || d.Remaining != (Counts{}) {
		t.Fatalf("detail = %+v", d)
	}
	recs, err := w.im.List(ctx)
	w.must(err)
	if len(recs) != 1 || recs[0].UndoneAt == nil {
		t.Fatalf("list = %+v", recs)
	}
	_, err = w.im.Detail(ctx, uuid.New())
	wantStatus(t, "unknown import", err, http.StatusNotFound)
}
