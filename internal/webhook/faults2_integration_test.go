//go:build integration

package webhook

import (
	"bytes"
	"mime/multipart"
	"net/textproto"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/testutil/pgfault"
)

func avatarForm() ([]byte, string) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	hd := textproto.MIMEHeader{}
	hd.Set("Content-Disposition", `form-data; name="avatar"; filename="a.png"`)
	hd.Set("Content-Type", "image/png")
	part, _ := mw.CreatePart(hd)
	_, _ = part.Write(png)
	_ = mw.Close()
	return buf.Bytes(), mw.FormDataContentType()
}

// The answer of a rename or avatar change is read back after the change.
func TestResponseReadBackFailures(t *testing.T) {
	f := newFixture(t)
	rec, _ := f.newHook("hook")
	admin := "/admin/webhooks/" + rec.ID.String()
	h := f.faulty(&pgfault.Tracer{Query: "w.id = $1", Skip: 1})
	wantStatus(t, f.json(h, "PATCH", admin, `{"name":"n"}`), 500, "rename read-back")

	form, ct := avatarForm()
	h = f.faulty(&pgfault.Tracer{Query: "w.id = $1", Skip: 1})
	wantStatus(t, f.serve(h, "POST", admin+"/avatar", bytes.NewReader(form), map[string]string{"Content-Type": ct}), 500, "avatar read-back")
	h = f.faulty(&pgfault.Tracer{Query: "w.id = $1"})
	wantStatus(t, f.serve(h, "POST", admin+"/avatar", bytes.NewReader(form), map[string]string{"Content-Type": ct}), 500, "avatar load")
	h = f.faulty(&pgfault.Tracer{Query: "w.id = $1"})
	wantStatus(t, f.serve(h, "POST", admin+"/token", nil, nil), 500, "token load")
}

func TestExecuteMultipartEdges(t *testing.T) {
	f := newFixture(t)
	rec, token := f.newHook("hook")
	path := "/webhooks/" + rec.ID.String() + "/" + token
	// A broken part header ends the form.
	w := f.serve(f.h, "POST", path, strings.NewReader("--zz\r\nnot a header\r\n\r\nx\r\n--zz--\r\n"), map[string]string{"Content-Type": "multipart/form-data; boundary=zz"})
	wantStatus(t, w, 400, "broken part header")

	// The media row cannot be written: the stored file goes again.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("payload_json", `{"content":"x"}`)
	hd := textproto.MIMEHeader{}
	hd.Set("Content-Disposition", `form-data; name="files[0]"; filename="a.png"`)
	hd.Set("Content-Type", "image/png")
	part, _ := mw.CreatePart(hd)
	_, _ = part.Write(png)
	_ = mw.Close()
	h := f.faulty(&pgfault.Tracer{Query: "INSERT INTO media"})
	wantStatus(t, f.serve(h, "POST", path, bytes.NewReader(buf.Bytes()), map[string]string{"Content-Type": mw.FormDataContentType()}), 500, "record file")
	if f.store.Len() != 0 {
		t.Fatalf("file left behind: %d", f.store.Len())
	}
}
