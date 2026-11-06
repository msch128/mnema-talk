//go:build integration

package communityevents

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func (f *rosterFixture) channel(t *testing.T, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := f.p.QueryRow(context.Background(), `INSERT INTO channels (name, type) VALUES ($1, 'text') RETURNING id`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *rosterFixture) coverIn(t *testing.T, channel *uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := f.p.QueryRow(context.Background(), `
		INSERT INTO media (uploader_id, channel_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes)
		VALUES ($1, $2, 'b', $3, 'cover.png', 'image/png', 3) RETURNING id`,
		f.creator.ID, channel, "covers/"+uuid.NewString()+".png").Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *rosterFixture) mediaChannel(t *testing.T, media uuid.UUID) *uuid.UUID {
	t.Helper()
	var ch *uuid.UUID
	if err := f.p.QueryRow(context.Background(), `SELECT channel_id FROM media WHERE id = $1`, media).Scan(&ch); err != nil {
		t.Fatal(err)
	}
	return ch
}

// Moving an event to another channel moves its cover with it, so members of
// the new channel can see the cover and members of the old one no longer can.
func TestMovingAnEventMovesItsCoverToTheNewChannel(t *testing.T) {
	f := newRosterFixture(t)
	ctx := context.Background()
	a, b := f.channel(t, "a"), f.channel(t, "b")
	cover := f.coverIn(t, nil)
	if _, err := f.s.Update(ctx, f.event.ID, UpdateEventRequest{ChannelID: &a}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.ReplaceCover(ctx, f.event.ID, cover); err != nil {
		t.Fatal(err)
	}

	if _, err := f.s.Update(ctx, f.event.ID, UpdateEventRequest{ChannelID: &b}); err != nil {
		t.Fatal(err)
	}

	if got := f.mediaChannel(t, cover); got == nil || *got != b {
		t.Errorf("cover channel = %v, want %v", got, b)
	}
}

// A cover uploaded while its event was moved is filed under the channel the
// event has when the cover is attached, not the one read before the upload.
func TestACoverTakesTheChannelOfTheEventItIsAttachedTo(t *testing.T) {
	f := newRosterFixture(t)
	ctx := context.Background()
	a, b := f.channel(t, "a"), f.channel(t, "b")
	if _, err := f.s.Update(ctx, f.event.ID, UpdateEventRequest{ChannelID: &b}); err != nil {
		t.Fatal(err)
	}
	stale := f.coverIn(t, &a)

	if _, err := f.s.ReplaceCover(ctx, f.event.ID, stale); err != nil {
		t.Fatal(err)
	}

	if got := f.mediaChannel(t, stale); got == nil || *got != b {
		t.Errorf("cover channel = %v, want %v", got, b)
	}
}
