//go:build integration

package media

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"
	"time"
)

// Lane 8 cycle test for uploads: a client that starts an upload and goes away
// in the middle of the body, at every byte offset of the request. After the
// rounds nothing may be left: no reservation (inventory I6 row 48), no stored
// object, no media or upload_usage row, no goroutine; and the account's budget
// is whole again. Smoke cuts at every offset of one small request;
// MEDIA_CYCLE_DEEP=1 adds a 256 KiB file cut at every 61st offset, across the
// read buffers' boundaries.

// cutReader gives the first n bytes of b, then fails as a dropped client does.
type cutReader struct {
	b []byte
	n int
}

var errClientGone = errors.New("client went away")

func (c *cutReader) Read(p []byte) (int, error) {
	if c.n <= 0 {
		return 0, errClientGone
	}
	k := copy(p, c.b[:min(len(c.b), c.n)])
	c.b, c.n = c.b[k:], c.n-k
	return k, nil
}

func (f *mediaFixture) countRows(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := f.p.QueryRow(context.Background(), `SELECT COUNT(*) FROM `+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestUploadsAbortedAtEveryOffsetLeaveNothingBehind(t *testing.T) {
	f := mediaDB(t)
	f.h.MaxUploadBytes = 1 << 20
	const budget = 1 << 20
	f.h.Quota = Quota{PerUserBytes: budget}
	type shape struct{ fileBytes, stride int }
	shapes := []shape{{2000, 1}}
	if os.Getenv("MEDIA_CYCLE_DEEP") == "1" {
		shapes = append(shapes, shape{256 << 10, 61})
	}

	objects, mediaRows, usageRows := f.store.Len(), f.countRows(t, "media"), f.countRows(t, "upload_usage")
	goBase := runtime.NumGoroutine()
	aborted, want := 0, 0
	for _, sh := range shapes {
		full := uploadRequest(t, formPart{name: "file", filename: "a.txt", mime: "text/plain", value: "plain text " + string(bytes.Repeat([]byte("a"), sh.fileBytes))})
		body, err := io.ReadAll(full.Body)
		if err != nil {
			t.Fatal(err)
		}
		contentType := full.Header.Get("Content-Type")
		for cut := 0; cut < len(body); cut += sh.stride {
			want++
			r := httptest.NewRequest("POST", "/upload", &cutReader{b: body, n: cut})
			r.Header.Set("Content-Type", contentType)
			r.ContentLength = int64(len(body))
			if err := f.h.upload(httptest.NewRecorder(), f.req(r, "channelID", f.channel.String())); err == nil {
				t.Fatalf("an upload cut at byte %d of %d succeeded", cut, len(body))
			}
			aborted++
		}
	}
	if aborted != want {
		t.Fatalf("only %d of %d aborted uploads ran", aborted, want)
	}

	f.h.reserved.mu.Lock()
	users, total := len(f.h.reserved.user), f.h.reserved.total
	f.h.reserved.mu.Unlock()
	if users != 0 || total != 0 {
		t.Errorf("reservations left after the aborted uploads: %d accounts, %d bytes", users, total)
	}
	if got := f.store.Len(); got != objects {
		t.Errorf("stored objects %d -> %d after aborted uploads", objects, got)
	}
	if got := f.countRows(t, "media"); got != mediaRows {
		t.Errorf("media rows %d -> %d after aborted uploads", mediaRows, got)
	}
	if got := f.countRows(t, "upload_usage"); got != usageRows {
		t.Errorf("upload_usage rows %d -> %d after aborted uploads", usageRows, got)
	}
	gor := runtime.NumGoroutine()
	for deadline := time.Now().Add(5 * time.Second); gor > goBase+2 && time.Now().Before(deadline); gor = runtime.NumGoroutine() {
		time.Sleep(20 * time.Millisecond)
	}
	if gor > goBase+2 {
		t.Errorf("goroutines %d after the aborted uploads, baseline %d", gor, goBase)
	}
	// The whole budget is there: a reservation of all of it passes.
	release, err := f.h.ReserveUpload(context.Background(), f.user.ID, budget)
	if err != nil {
		t.Fatalf("the budget is not whole after the aborted uploads: %v", err)
	}
	release()
	t.Logf("aborted uploads %d over %d request shapes; reservations, objects, rows, goroutines at baseline", aborted, len(shapes))
}
