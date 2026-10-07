package media

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/httpx"
)

type formPart struct{ name, filename, mime, value string }

func uploadRequest(t *testing.T, parts ...formPart) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for _, part := range parts {
		h := textproto.MIMEHeader{}
		disposition := `form-data; name="` + part.name + `"`
		if part.filename != "" {
			disposition += `; filename="` + part.filename + `"`
		}
		h.Set("Content-Disposition", disposition)
		if part.mime != "" {
			h.Set("Content-Type", part.mime)
		}
		out, err := w.CreatePart(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(out, part.value); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/upload", bytes.NewReader(body.Bytes()))
	r.Header.Set("Content-Type", w.FormDataContentType())
	return r
}

func requireAPIError(t *testing.T, err error, status int) {
	t.Helper()
	var api *httpx.APIError
	if !errors.As(err, &api) || api.Status != status {
		t.Fatalf("error = %v, want status %d", err, status)
	}
}

func TestReadFileValidatesMultipartAndSize(t *testing.T) {
	for _, tc := range []struct {
		name   string
		parts  []formPart
		max    int64
		status int
	}{
		{"missing", []formPart{{name: "caption", value: "notes"}}, 10, 400},
		{"empty", []formPart{{name: "avatar", filename: "a.png", mime: "image/png"}}, 10, 400},
		{"size", []formPart{{name: "avatar", filename: "a.png", mime: "image/png", value: string(pngBytes)}}, 1, 413},
		{"mime", []formPart{{name: "avatar", filename: "a.png", mime: "image/png", value: string(htmlBytes)}}, 1000, 415},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := readFile(uploadRequest(t, tc.parts...), "avatar", tc.max, avatarMIME, true)
			requireAPIError(t, err, tc.status)
		})
	}
	r := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader("bad"))
	_, err := readFile(r, "avatar", 100, avatarMIME, true)
	requireAPIError(t, err, 400)
	r = uploadRequest(t, formPart{name: "avatar", filename: "a.png", mime: "image/png", value: string(pngBytes)})
	r.Body = http.MaxBytesReader(httptest.NewRecorder(), r.Body, 10)
	_, err = readFile(r, "avatar", 100, avatarMIME, true)
	requireAPIError(t, err, 413)
	r = uploadRequest(t, formPart{name: "avatar", filename: ".", mime: "image/png", value: string(pngBytes)})
	in, err := readFile(r, "avatar", 100, avatarMIME, true)
	if err != nil {
		t.Fatal(err)
	}
	defer in.file.Close()
	data, err := io.ReadAll(in.file)
	if err != nil || !bytes.Equal(data, pngBytes) || in.filename != "file" || in.mime != "image/png" {
		t.Fatalf("parsed file=%+v, body=%q, error=%v", in, data, err)
	}
}

func TestStreamFormRejectsInvalidAndDiscardsStoredFiles(t *testing.T) {
	for _, tc := range []struct {
		name   string
		parts  []formPart
		status int
	}{
		{"missing file", []formPart{{name: "content", value: "caption"}}, 400},
		{"empty file", []formPart{{name: "file", filename: "empty.txt", mime: "text/plain"}}, 400},
		{"invalid caption", []formPart{{name: "content", value: strings.Repeat("x", 4001)}, {name: "file", filename: "a.txt", mime: "text/plain", value: "text"}}, 400},
		{"bad parent", []formPart{{name: "parent_id", value: "bad"}, {name: "file", filename: "a.txt", mime: "text/plain", value: "text"}}, 400},
		{"late invalid caption", []formPart{{name: "file", filename: "a.txt", mime: "text/plain", value: "text"}, {name: "content", value: strings.Repeat("x", 4001)}}, 400},
		{"late bad reply", []formPart{{name: "file", filename: "a.txt", mime: "text/plain", value: "text"}, {name: "reply_to_id", value: "bad"}}, 400},
		{"two files", []formPart{{name: "file", filename: "a.txt", mime: "text/plain", value: "text"}, {name: "file", filename: "b.txt", mime: "text/plain", value: "text"}}, 400},
		{"oversized field", []formPart{{name: "file", filename: "a.txt", mime: "text/plain", value: "text"}, {name: "content", value: strings.Repeat("x", maxFieldBytes+1)}}, 400},
		{"oversized file", []formPart{{name: "file", filename: "a.txt", mime: "text/plain", value: strings.Repeat("x", 1025)}}, 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := NewMemoryStore()
			h := &Handler{Store: store, MaxUploadBytes: 1024}
			in, target, err := h.readUpload(uploadRequest(t, tc.parts...), uuid.New())
			requireAPIError(t, err, tc.status)
			if in != nil || target != nil || store.Len() != 0 {
				t.Fatalf("rejected form left in=%v target=%v objects=%d", in, target, store.Len())
			}
		})
	}
	store := NewMemoryStore()
	h := &Handler{Store: store, MaxUploadBytes: 1024}
	r := uploadRequest(t, formPart{name: "unknown", value: "ignored"}, formPart{name: "file", filename: ".", mime: "text/plain", value: "hello"}, formPart{name: "content", value: " caption "})
	in, target, err := h.readUpload(r, uuid.New())
	if err != nil || in.filename != "file" || in.size != 5 || target.content != "caption" || store.Len() != 1 {
		t.Fatalf("in=%+v target=%+v error=%v", in, target, err)
	}
}

func TestStreamFormMapsTransportAndFramingFailures(t *testing.T) {
	h := &Handler{Store: NewMemoryStore(), MaxUploadBytes: 1024}
	_, _, err := h.readUpload(httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader("not multipart")), uuid.New())
	requireAPIError(t, err, 400)
	for _, content := range []string{
		"garbage",
		"--boundary\r\nContent-Disposition: form-data; name=\"content\"\r\n\r\nunfinished",
		"--boundary\r\nContent-Disposition: form-data; name=\"file\"; filename=\"a.txt\"\r\nContent-Type: text/plain\r\n\r\nunfinished",
		"--boundary\r\nContent-Disposition: form-data; name=\"file\"; filename=\"a.txt\"\r\nContent-Type: text/plain\r\n\r\n" + strings.Repeat("x", 600),
	} {
		r := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader(content))
		r.Header.Set("Content-Type", "multipart/form-data; boundary=boundary")
		_, _, err := h.readUpload(r, uuid.New())
		requireAPIError(t, err, 400)
	}
	// The first file is complete, but the following part has a malformed header.
	raw := "--boundary\r\nContent-Disposition: form-data; name=\"file\"; filename=\"a.txt\"\r\nContent-Type: text/plain\r\n\r\nnotes\r\n--boundary\r\nmalformed header\r\n\r\n"
	broken := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader(raw))
	broken.Header.Set("Content-Type", "multipart/form-data; boundary=boundary")
	_, _, err = h.readUpload(broken, uuid.New())
	requireAPIError(t, err, 400)
	if h.Store.(*MemoryStore).Len() != 0 {
		t.Fatal("malformed later part left a stored file")
	}
	r := uploadRequest(t, formPart{name: "file", filename: "a.txt", mime: "text/plain", value: strings.Repeat("x", 600)})
	r.Body = http.MaxBytesReader(httptest.NewRecorder(), r.Body, 400)
	_, _, err = h.readUpload(r, uuid.New())
	requireAPIError(t, err, 413)
}

type failingMediaStore struct {
	*MemoryStore
	uploadErr, deleteErr, listErr, batchErr error
	afterUpload, afterDelete, afterBatch    func()
}

func (s *failingMediaStore) Upload(ctx context.Context, key string, body io.Reader, mime string, size int64) error {
	if s.uploadErr != nil {
		return s.uploadErr
	}
	err := s.MemoryStore.Upload(ctx, key, body, mime, size)
	if s.afterUpload != nil {
		s.afterUpload()
	}
	return err
}
func (s *failingMediaStore) Delete(ctx context.Context, key string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if s.deleteErr != nil {
		return s.deleteErr
	}
	err := s.MemoryStore.Delete(ctx, key)
	if s.afterDelete != nil {
		s.afterDelete()
	}
	return err
}
func (s *failingMediaStore) DeleteBatch(ctx context.Context, keys []string) error {
	if s.batchErr != nil {
		return s.batchErr
	}
	err := s.MemoryStore.DeleteBatch(ctx, keys)
	if s.afterBatch != nil {
		s.afterBatch()
	}
	return err
}
func (s *failingMediaStore) List(ctx context.Context, prefix string, fn func(ObjectInfo) error) error {
	if s.listErr != nil {
		return s.listErr
	}
	return s.MemoryStore.List(ctx, prefix, fn)
}

func TestStorePartAndDiscardReportStorageFailures(t *testing.T) {
	sentinel := errors.New("storage unavailable")
	store := &failingMediaStore{MemoryStore: NewMemoryStore(), uploadErr: sentinel, deleteErr: sentinel}
	h := &Handler{Store: store, MaxUploadBytes: 1024}
	_, _, err := h.readUpload(uploadRequest(t, formPart{name: "file", filename: "a.txt", mime: "text/plain", value: "notes"}), uuid.New())
	if !errors.Is(err, sentinel) || !strings.Contains(err.Error(), "store upload") {
		t.Fatalf("store error: %v", err)
	}
	h.discard(context.Background(), "orphan")
	store.deleteErr = nil
	store.Put("discard", []byte("x"), time.Now())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h.discard(ctx, "discard")
	if store.Has("discard") {
		t.Fatal("cancellation prevented orphan cleanup")
	}
}
