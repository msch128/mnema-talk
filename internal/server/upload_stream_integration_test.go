//go:build integration

package server

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/config"
)

// formPart is one part of a hand-built upload form, in order.
type formPart struct {
	name, filename, contentType string
	data                        []byte
}

// uploadParts posts the parts in exactly this order.
func (c *client) uploadParts(path string, parts ...formPart) response {
	c.a.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, p := range parts {
		if p.filename == "" {
			_ = mw.WriteField(p.name, string(p.data))
			continue
		}
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", `form-data; name="`+p.name+`"; filename="`+p.filename+`"`)
		h.Set("Content-Type", p.contentType)
		w, _ := mw.CreatePart(h)
		_, _ = w.Write(p.data)
	}
	_ = mw.Close()
	return c.do(http.MethodPost, path, &buf, map[string]string{"Content-Type": mw.FormDataContentType()})
}

func pngFile(extra int) formPart {
	return formPart{name: "file", filename: "a.png", contentType: "image/png", data: append(append([]byte{}, pngBytes...), make([]byte, extra)...)}
}

func field(name, value string) formPart { return formPart{name: name, data: []byte(value)} }

func TestStreamedUploadReadsFieldsOnEitherSide(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "general", "text")
	path := "/api/channels/" + ch.String() + "/upload"
	root := a.send(admin, ch, "root")

	// Caption and thread before the file (what the web app sends) ...
	var msg struct {
		Content  string     `json:"content"`
		ParentID *uuid.UUID `json:"parent_id"`
	}
	res := admin.uploadParts(path, field("content", "before"), field("parent_id", root.String()), pngFile(0))
	if res.status != http.StatusCreated {
		t.Fatalf("fields before the file: %d %s", res.status, res.body)
	}
	res.decode(t, &msg)
	if msg.Content != "before" || msg.ParentID == nil || *msg.ParentID != root {
		t.Errorf("fields before the file: %+v", msg)
	}
	// ... and after it, which other clients may do.
	res = admin.uploadParts(path, pngFile(0), field("content", "after"), field("parent_id", root.String()))
	if res.status != http.StatusCreated {
		t.Fatalf("fields after the file: %d %s", res.status, res.body)
	}
	res.decode(t, &msg)
	if msg.Content != "after" || msg.ParentID == nil || *msg.ParentID != root {
		t.Errorf("fields after the file: %+v", msg)
	}
	if n := a.store.Len(); n != 2 {
		t.Errorf("%d objects stored, want 2", n)
	}
}

func TestStreamedUploadStoresNothingOnRejection(t *testing.T) {
	a := newAppWithConfig(t, func(c *config.Config) { c.MaxUploadMB = 1 })
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "general", "text")
	path := "/api/channels/" + ch.String() + "/upload"

	for _, tc := range []struct {
		name  string
		parts []formPart
		want  int
	}{
		{"bad parent before the file", []formPart{field("parent_id", uuid.NewString()), pngFile(0)}, http.StatusBadRequest},
		// Stored first, then the form turns out invalid: the object must go.
		{"bad parent after the file", []formPart{pngFile(0), field("parent_id", "not-a-uuid")}, http.StatusBadRequest},
		{"unknown parent after the file", []formPart{pngFile(0), field("parent_id", uuid.NewString())}, http.StatusBadRequest},
		{"too large", []formPart{pngFile(1 << 20)}, http.StatusRequestEntityTooLarge},
		{"empty file", []formPart{{name: "file", filename: "a.png", contentType: "image/png"}}, http.StatusBadRequest},
		{"no file", []formPart{field("content", "hi")}, http.StatusBadRequest},
		{"two files", []formPart{pngFile(0), pngFile(0)}, http.StatusBadRequest},
		{"disguised html", []formPart{{name: "file", filename: "a.png", contentType: "image/png", data: xssHTML}}, http.StatusUnsupportedMediaType},
	} {
		if res := admin.uploadParts(path, tc.parts...); res.status != tc.want {
			t.Errorf("%s: %d %s, want %d", tc.name, res.status, res.body, tc.want)
		}
		if n := a.store.Len(); n != 0 {
			t.Fatalf("%s: %d objects left in storage", tc.name, n)
		}
	}

	// Just under the limit still goes through.
	if res := admin.uploadParts(path, pngFile(1<<20-len(pngBytes))); res.status != http.StatusCreated {
		t.Fatalf("file at the limit: %d %s", res.status, res.body)
	}
}
