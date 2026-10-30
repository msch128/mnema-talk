//go:build integration

package server

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/media"
	"github.com/msch128/mnema-talk/internal/perm"
)

type stagedFile struct {
	ID        uuid.UUID `json:"id"`
	SizeBytes int64     `json:"size_bytes"`
	URL       string    `json:"url"`
}

type sentMessage struct {
	ID          uuid.UUID `json:"id"`
	Content     string    `json:"content"`
	Attachments []struct {
		ID               uuid.UUID `json:"id"`
		OriginalFilename string    `json:"original_filename"`
	} `json:"attachments"`
}

// stage uploads one picture as a staged attachment and returns it.
func (a *app) stage(c *client, channel uuid.UUID, name string) stagedFile {
	a.t.Helper()
	res := c.upload("/api/channels/"+channel.String()+"/attachments", "file", name, "image/png", pngBytes, nil)
	if res.status != http.StatusCreated {
		a.t.Fatalf("stage %s: %d %s", name, res.status, res.body)
	}
	var f stagedFile
	res.decode(a.t, &f)
	return f
}

func sendWith(c *client, channel uuid.UUID, content string, ids ...uuid.UUID) response {
	return c.post("/api/channels/"+channel.String()+"/messages", map[string]any{"content": content, "attachment_ids": ids})
}

func TestAMessageCarriesSeveralAttachmentsInTheOrderGiven(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	bob := a.register(admin, "bob")
	ch := a.createChannel(admin, "medien", "text")

	before := len(a.events.Snapshot())
	first, second, third := a.stage(admin, ch, "one.png"), a.stage(admin, ch, "two.png"), a.stage(admin, ch, "three.png")
	if res := bob.get(first.URL); res.status != http.StatusNotFound {
		t.Fatalf("an unsent file belongs to its uploader: %d", res.status)
	}
	if res := admin.get(first.URL); res.status != http.StatusOK {
		t.Fatalf("the uploader sees the staged file: %d", res.status)
	}
	if n := len(a.events.Snapshot()) - before; n != 0 {
		t.Fatalf("staging posts nothing, got %d events", n)
	}

	res := sendWith(admin, ch, "", third.ID, first.ID, second.ID)
	if res.status != http.StatusCreated {
		t.Fatalf("send: %d %s", res.status, res.body)
	}
	var msg sentMessage
	res.decode(t, &msg)
	var got []string
	for _, at := range msg.Attachments {
		got = append(got, at.OriginalFilename)
	}
	if fmt.Sprint(got) != "[three.png one.png two.png]" {
		t.Fatalf("attachments %v, want the order they were sent in", got)
	}
	if res := bob.get(first.URL); res.status != http.StatusOK {
		t.Fatalf("once sent, the channel sees it: %d", res.status)
	}
	// The history gives the same order.
	hist := bob.get("/api/channels/" + ch.String() + "/messages")
	var page []sentMessage
	hist.decode(t, &page)
	if len(page) != 1 || len(page[0].Attachments) != 3 || page[0].Attachments[0].OriginalFilename != "three.png" {
		t.Fatalf("history: %s", hist.body)
	}
	if res := sendWith(admin, ch, "again", first.ID); res.status != http.StatusBadRequest {
		t.Fatalf("a file is sent once: %d %s", res.status, res.body)
	}
}

func TestAttachmentLimitsAndOwnership(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	bob := a.register(admin, "bob")
	ch := a.createChannel(admin, "medien", "text")
	other := a.createChannel(admin, "andere", "text")

	var eleven []uuid.UUID
	for range 11 {
		eleven = append(eleven, uuid.New())
	}
	if res := sendWith(admin, ch, "x", eleven...); res.status != http.StatusBadRequest {
		t.Errorf("eleven attachments: %d", res.status)
	}
	f := a.stage(admin, ch, "a.png")
	if res := sendWith(admin, ch, "x", f.ID, f.ID); res.status != http.StatusBadRequest {
		t.Errorf("duplicate id: %d", res.status)
	}
	if res := sendWith(bob, ch, "x", f.ID); res.status != http.StatusBadRequest {
		t.Errorf("someone else's file: %d", res.status)
	}
	if res := sendWith(admin, other, "x", f.ID); res.status != http.StatusBadRequest {
		t.Errorf("a file staged for another channel: %d", res.status)
	}
	if res := sendWith(admin, ch, "x", uuid.New()); res.status != http.StatusBadRequest {
		t.Errorf("unknown id: %d", res.status)
	}
	if res := sendWith(admin, ch, ""); res.status != http.StatusBadRequest {
		t.Errorf("no text and no files: %d", res.status)
	}
	// Nothing was attached by the refused attempts.
	var n int
	_ = a.db.QueryRow(context.Background(), `SELECT count(*) FROM media WHERE staged`).Scan(&n)
	if n != 1 {
		t.Errorf("the refused sends must leave the file staged, got %d staged", n)
	}

	// Ten files go, together at most 100 MB.
	var ten []uuid.UUID
	for i := range 10 {
		ten = append(ten, a.stage(admin, ch, fmt.Sprintf("%d.png", i)).ID)
	}
	if _, err := a.db.Exec(context.Background(), `UPDATE media SET size_bytes = 15 << 20 WHERE id = ANY($1)`, ten); err != nil {
		t.Fatal(err)
	}
	if res := sendWith(admin, ch, "heavy", ten...); res.status != http.StatusRequestEntityTooLarge {
		t.Errorf("150 MB in one message: %d %s", res.status, res.body)
	}
	if _, err := a.db.Exec(context.Background(), `UPDATE media SET size_bytes = 5 << 20 WHERE id = ANY($1)`, ten); err != nil {
		t.Fatal(err)
	}
	if res := sendWith(admin, ch, "ten", ten...); res.status != http.StatusCreated {
		t.Errorf("ten files: %d %s", res.status, res.body)
	}
}

func TestAttachmentsNeedTheAttachFilesPermission(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	bob := a.register(admin, "bob")
	ch := a.createChannel(admin, "medien", "text")
	f := a.stage(bob, ch, "ok.png")

	if res := putOverwrites(admin, "channels", ch, overwrite("role", everyoneID(t, admin), 0, perm.AttachFiles)); res.status != http.StatusNoContent {
		t.Fatalf("deny: %d", res.status)
	}
	res := bob.upload("/api/channels/"+ch.String()+"/attachments", "file", "no.png", "image/png", pngBytes, nil)
	if res.status != http.StatusForbidden {
		t.Errorf("staging without Attach files: %d %s", res.status, res.body)
	}
	if res := sendWith(bob, ch, "hi", f.ID); res.status != http.StatusForbidden {
		t.Errorf("sending files without Attach files: %d %s", res.status, res.body)
	}
	if res := sendWith(bob, ch, "plain text still works"); res.status != http.StatusCreated {
		t.Errorf("text only: %d %s", res.status, res.body)
	}
}

func TestStagedFilesCanBeRemovedAndAreCappedPerMember(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	bob := a.register(admin, "bob")
	ch := a.createChannel(admin, "medien", "text")

	f := a.stage(bob, ch, "a.png")
	path := "/api/channels/" + ch.String() + "/attachments/" + f.ID.String()
	if res := admin.delete(path); res.status != http.StatusNotFound {
		t.Errorf("only the uploader removes it: %d", res.status)
	}
	if res := bob.delete(path); res.status != http.StatusNoContent {
		t.Fatalf("remove: %d %s", res.status, res.body)
	}
	if res := bob.get(f.URL); res.status != http.StatusNotFound {
		t.Errorf("a removed file is gone: %d", res.status)
	}
	if a.store.Len() != 0 {
		t.Errorf("the object is deleted with it: %d left", a.store.Len())
	}
	// A sent file cannot be removed through the staging route.
	sent := a.stage(bob, ch, "b.png")
	sendWith(bob, ch, "x", sent.ID)
	if res := bob.delete("/api/channels/" + ch.String() + "/attachments/" + sent.ID.String()); res.status != http.StatusNotFound {
		t.Errorf("remove a sent file: %d", res.status)
	}

	for range 30 {
		a.stage(bob, ch, "c.png")
	}
	res := bob.upload("/api/channels/"+ch.String()+"/attachments", "file", "d.png", "image/png", pngBytes, nil)
	if res.status != http.StatusBadRequest {
		t.Errorf("31st unsent file: %d %s", res.status, res.body)
	}
}

func TestUnsentStagedFilesAreSweptAfterTheTimeout(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "medien", "text")
	ctx := context.Background()

	stale, fresh, sent := a.stage(admin, ch, "stale.png"), a.stage(admin, ch, "fresh.png"), a.stage(admin, ch, "sent.png")
	sendWith(admin, ch, "x", sent.ID)
	if _, err := a.db.Exec(ctx, `UPDATE media SET created_at = NOW() - INTERVAL '7 hours' WHERE id = ANY($1)`, []uuid.UUID{stale.ID, sent.ID}); err != nil {
		t.Fatal(err)
	}
	n, err := media.SweepStaged(ctx, a.db, a.store, 6*time.Hour)
	if err != nil || n != 1 {
		t.Fatalf("swept %d, %v; want only the stale unsent file", n, err)
	}
	if res := admin.get(stale.URL); res.status != http.StatusNotFound {
		t.Errorf("stale file: %d", res.status)
	}
	for _, f := range []stagedFile{fresh, sent} {
		if res := admin.get(f.URL); res.status != http.StatusOK {
			t.Errorf("%s must survive: %d", f.URL, res.status)
		}
	}
	if a.store.Len() != 2 {
		t.Errorf("the swept object leaves storage: %d objects", a.store.Len())
	}
	if res := sendWith(admin, ch, "late", stale.ID); res.status != http.StatusBadRequest {
		t.Errorf("sending a swept file: %d", res.status)
	}
}

func TestTheSingleFileUploadStillWorks(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "medien", "text")

	res := admin.upload("/api/channels/"+ch.String()+"/upload", "file", "old.png", "image/png", pngBytes, map[string]string{"content": "from an old client"})
	if res.status != http.StatusCreated {
		t.Fatalf("legacy upload: %d %s", res.status, res.body)
	}
	var msg sentMessage
	res.decode(t, &msg)
	if len(msg.Attachments) != 1 || msg.Content != "from an old client" {
		t.Fatalf("message: %s", res.body)
	}
}
