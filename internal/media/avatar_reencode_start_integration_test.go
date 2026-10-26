//go:build integration

package media

import (
	"context"
	"errors"
	"testing"
)

func TestReencodeJobDoesNothingWithoutStorage(t *testing.T) {
	f := mediaDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	StartAvatarReencode(ctx, f.p, nil, "bucket", nil, nil)
	StartAvatarReencode(ctx, f.p, f.store, "bucket", nil, nil) // waits for its first run; cancelled with the context
}

func TestAvatarThatCannotBeWrittenIsRetriedLater(t *testing.T) {
	f := mediaDB(t)
	_, key := f.oldAvatar(t, jpegWithEXIF(t), "image/jpeg")
	f.store.uploadErr = errors.New("storage full")
	done, failed, err := f.h.ReencodeAvatars(context.Background())
	if err != nil || done != 0 || failed != 1 {
		t.Fatalf("job: done=%d failed=%d err=%v", done, failed, err)
	}
	if got, marked := f.avatarState(t); got != key || marked || !f.store.Has(key) {
		t.Fatal("the old avatar stays and is not marked")
	}
}

func TestReencodeStopsWhenTheServerStops(t *testing.T) {
	f := mediaDB(t)
	f.oldAvatar(t, jpegWithEXIF(t), "image/jpeg")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := f.h.ReencodeAvatars(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}
