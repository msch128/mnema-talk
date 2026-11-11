//go:build integration

package media

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestMediaUploadValidationAndFailedDatabaseWrite(t *testing.T) {
	f := mediaDB(t)
	for _, id := range []string{"bad", uuid.NewString()} {
		err := f.h.upload(httptest.NewRecorder(), f.req(httptest.NewRequest(http.MethodPost, "/", nil), "channelID", id))
		if id == "bad" {
			requireAPIError(t, err, 400)
		} else {
			requireAPIError(t, err, 404)
		}
	}
	err := f.h.upload(httptest.NewRecorder(), f.req(httptest.NewRequest(http.MethodPost, "/", strings.NewReader("bad")), "channelID", f.channel.String()))
	requireAPIError(t, err, 400)
	// A concurrent account deletion after storage completes must roll back the message and discard its object.
	f.store.afterUpload = func() { execMedia(t, f.p, `DELETE FROM users WHERE id=$1`, f.user.ID) }
	r := f.req(uploadRequest(t, formPart{name: "file", filename: "a.txt", mime: "text/plain", value: "abc"}), "channelID", f.channel.String())
	err = f.h.upload(httptest.NewRecorder(), r)
	if err == nil || !strings.Contains(err.Error(), "record upload") || f.store.Len() != 0 {
		t.Fatalf("failed insert: %v, objects=%d", err, f.store.Len())
	}
}

func TestValidateUploadTargetLoadsThreadAndRejectsMissingTarget(t *testing.T) {
	f := mediaDB(t)
	root := uuid.New()
	execMedia(t, f.p, `INSERT INTO messages(id,channel_id,user_id,content) VALUES($1,$2,$3,'root')`, root, f.channel, f.user.ID)
	target, err := f.h.validateTarget(context.Background(), f.channel, map[string]string{"parent_id": root.String(), "reply_to_id": root.String()})
	if err != nil || target.parentID == nil || *target.parentID != root || target.replyToID == nil || *target.replyToID != root {
		t.Fatalf("thread root quote=%+v %v", target, err)
	}
	target, err = f.h.validateTarget(context.Background(), f.channel, map[string]string{"parent_id": root.String()})
	if err != nil || target.parentID == nil || *target.parentID != root {
		t.Fatalf("valid thread=%+v %v", target, err)
	}
	_, err = f.h.validateTarget(context.Background(), f.channel, map[string]string{"parent_id": uuid.NewString()})
	requireAPIError(t, err, 400)
}
