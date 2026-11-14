//go:build integration

package media

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/events"
)

// Removing a file announces its message, and only a message clients may still
// see: not for an avatar, not for a message whose lifetime has ended, and
// nothing when the message cannot be read.
func TestAnnouncingARemovedFileNamesOnlyAVisibleMessage(t *testing.T) {
	f := mediaDB(t)
	rec := f.h.Events.(*events.Recorder)
	ctx := context.Background()
	shown := f.attachment(t, "uploads/shown", false)
	avatar := f.attachment(t, "avatars/someone", true)
	// A message whose lifetime ended a minute ago and that the sweep has not reached yet.
	endedMessage, ended := uuid.New(), uuid.New()
	execMedia(t, f.p, `INSERT INTO messages(id,channel_id,user_id,content,created_at,expires_at)
		VALUES($1,$2,$3,'short-lived',now() - interval '2 minutes',now() - interval '1 minute')`, endedMessage, f.channel, f.user.ID)
	execMedia(t, f.p, `INSERT INTO media(id,uploader_id,channel_id,message_id,s3_bucket,s3_key,original_filename,mime_type,size_bytes)
		VALUES($1,$2,$3,$4,'test-media','uploads/ended','a.txt','text/plain',3)`, ended, f.user.ID, f.channel, endedMessage)
	var shownMessage uuid.UUID
	if err := f.p.QueryRow(ctx, `SELECT message_id FROM media WHERE id = $1`, shown).Scan(&shownMessage); err != nil {
		t.Fatal(err)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	f.h.announceRemoved(cancelled, shown) // a failed lookup sends nothing
	for _, id := range []uuid.UUID{avatar, ended, shown} {
		f.h.announceRemoved(ctx, id)
	}

	got := rec.Snapshot()
	if len(got) != 1 || got[0].Type != "message_update" || got[0].ChannelID != f.channel {
		t.Fatalf("events=%+v", got)
	}
	if upd, ok := got[0].Payload.(*core.MessageUpdate); !ok || upd.ID != shownMessage {
		t.Fatalf("message_update names %+v, want %s", got[0].Payload, shownMessage)
	}
}
