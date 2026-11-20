//go:build integration

package media

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/mnema-xyz/talk/internal/db"
	"github.com/mnema-xyz/talk/internal/testutil/pgfault"
)

// An upload whose message committed but whose durability wait failed keeps
// its file: the message and its media row are stored and readable, so the
// object must not be deleted under them (db.ErrNotDurable).
func TestAnUploadCommittedButNotConfirmedKeepsItsFile(t *testing.T) {
	f := mediaDB(t)
	f.h.DB = &db.Pool{Pool: pgfault.Pool(t, f.p.Pool, &pgfault.Tracer{Query: "pg_logical_emit_message"})}
	r := f.req(uploadRequest(t, formPart{name: "file", filename: "a.txt", mime: "text/plain", value: "abc"}), "channelID", f.channel.String())
	if err := f.h.upload(httptest.NewRecorder(), r); err == nil {
		t.Fatal("the failed durability wait was not reported")
	}
	var rows int
	if err := f.p.QueryRow(context.Background(), `SELECT COUNT(*) FROM media WHERE channel_id = $1`, f.channel).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("media rows %d, %v: the upload should be stored", rows, err)
	}
	if f.store.Len() != 1 {
		t.Fatalf("objects %d: the stored message's file was deleted", f.store.Len())
	}
}
