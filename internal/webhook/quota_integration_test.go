//go:build integration

package webhook

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/textproto"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/media"
)

func uploadForm() ([]byte, string) { return uploadFormWith(`{"content":"x"}`) }

func uploadFormWith(payload string) ([]byte, string) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("payload_json", payload)
	hd := textproto.MIMEHeader{}
	hd.Set("Content-Disposition", `form-data; name="files[0]"; filename="a.png"`)
	hd.Set("Content-Type", "image/png")
	part, _ := mw.CreatePart(hd)
	_, _ = part.Write(png)
	_ = mw.Close()
	return buf.Bytes(), mw.FormDataContentType()
}

// The webhook upload path obeys the account budget: over it is 429, an unknown
// length reserves the worst case, and a failed post gives its reservation back.
func TestWebhookUploadQuota(t *testing.T) {
	f := newFixture(t)
	rec, token := f.newHook("hook")
	path := "/webhooks/" + rec.ID.String() + "/" + token
	body, ct := uploadForm()
	hdr := map[string]string{"Content-Type": ct}

	// Over its daily budget: refused before anything is stored.
	f.h.Media.Quota = media.Quota{PerUserBytes: 5000}
	if _, err := f.p.Exec(t.Context(), `INSERT INTO media(id,uploader_id,s3_bucket,s3_key,original_filename,mime_type,size_bytes) VALUES($1,$2,'b','k','a','text/plain',4990)`, uuid.New(), rec.botUserID); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, f.serve(f.h, "POST", path, bytes.NewReader(body), hdr), 429, "over the daily budget")
	if f.store.Len() != 0 {
		t.Fatalf("a refused upload was stored: %d", f.store.Len())
	}
	if _, err := f.p.Exec(t.Context(), `DELETE FROM media`); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, f.serve(f.h, "POST", path, bytes.NewReader(body), hdr), 204, "within the budget")

	// No Content-Length: the worst case (the whole body limit) is reserved, so a
	// small budget refuses it even though the real body is tiny.
	if _, err := f.p.Exec(t.Context(), `DELETE FROM media`); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, f.serve(f.h, "POST", path, io.MultiReader(bytes.NewReader(body)), hdr), 429, "unknown length")

	// A budget that fits exactly one worst-case request: a post that fails
	// gives its reservation back, so the next one still fits.
	worst := int64(1<<20) + 1<<20
	f.h.Media.Quota = media.Quota{PerUserBytes: worst}
	bad, badCT := uploadFormWith(`{"content"`)
	wantStatus(t, f.serve(f.h, "POST", path, io.MultiReader(bytes.NewReader(bad)), map[string]string{"Content-Type": badCT}), 400, "failing post")
	wantStatus(t, f.serve(f.h, "POST", path, io.MultiReader(bytes.NewReader(body)), hdr), 204, "reservation released after failure")
}
