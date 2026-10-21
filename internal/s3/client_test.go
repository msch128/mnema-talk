package s3

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/msch128/mnema-talk/internal/config"
)

// fakeS3 is a minimal path-style S3 endpoint: buckets, objects, ranged GETs
// and multi-object deletes. Keys starting with "locked/" refuse deletion.
type fakeS3 struct {
	mu           sync.Mutex
	buckets      map[string]bool
	objects      map[string][]byte
	types        map[string]string
	batchDeletes int
	deny         bool // answer everything with 403 AccessDenied
}

func newFakeS3(t *testing.T, buckets ...string) (*fakeS3, *httptest.Server) {
	f := &fakeS3{buckets: map[string]bool{}, objects: map[string][]byte{}, types: map[string]string{}}
	for _, b := range buckets {
		f.buckets[b] = true
	}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeS3) handleBucketLevel(w http.ResponseWriter, r *http.Request, bucket string) {
	switch {
	case r.Method == http.MethodHead:
		if !f.buckets[bucket] {
			w.WriteHeader(http.StatusNotFound)
		}
	case r.Method == http.MethodPut:
		f.buckets[bucket] = true
	case r.Method == http.MethodPost && r.URL.Query().Has("delete"):
		f.deleteObjects(w, r, bucket)
	default:
		w.WriteHeader(http.StatusNotImplemented)
	}
}

func (f *fakeS3) handleObjectPut(w http.ResponseWriter, r *http.Request, id string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	f.objects[id] = body
	f.types[id] = r.Header.Get("Content-Type")
}

func (f *fakeS3) handleObjectGet(w http.ResponseWriter, r *http.Request, id string) {
	body, ok := f.objects[id]
	if !ok {
		writeS3Error(w, http.StatusNotFound, "NoSuchKey")
		return
	}
	from, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(r.Header.Get("Range"), "bytes="), "-"))
	if err != nil || from < 0 || from > len(body) {
		from = 0
	}
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", from, len(body)-1, len(body)))
	w.WriteHeader(http.StatusPartialContent)
	if _, err := w.Write(body[from:]); err != nil {
		return
	}
}

func (f *fakeS3) handleObjectLevel(w http.ResponseWriter, r *http.Request, bucket, key string) {
	if !f.buckets[bucket] {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket")
		return
	}
	id := bucket + "/" + key
	switch r.Method {
	case http.MethodPut:
		f.handleObjectPut(w, r, id)
	case http.MethodDelete:
		delete(f.objects, id)
		w.WriteHeader(http.StatusNoContent)
	case http.MethodGet:
		f.handleObjectGet(w, r, id)
	default:
		w.WriteHeader(http.StatusNotImplemented)
	}
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deny {
		writeS3Error(w, http.StatusForbidden, "AccessDenied")
		return
	}
	bucket, key, hasKey := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if !hasKey || key == "" {
		f.handleBucketLevel(w, r, bucket)
		return
	}
	f.handleObjectLevel(w, r, bucket, key)
}

func (f *fakeS3) deleteObjects(w http.ResponseWriter, r *http.Request, bucket string) {
	f.batchDeletes++
	var req struct {
		Objects []struct{ Key string } `xml:"Object"`
	}
	if err := xml.NewDecoder(r.Body).Decode(&req); err != nil {
		writeS3Error(w, http.StatusBadRequest, "MalformedXML")
		return
	}
	var failed strings.Builder
	for _, o := range req.Objects {
		if strings.HasPrefix(o.Key, "locked/") {
			fmt.Fprintf(&failed, "<Error><Key>%s</Key><Code>AccessDenied</Code><Message>locked</Message></Error>", o.Key)
			continue
		}
		delete(f.objects, bucket+"/"+o.Key)
	}
	w.Header().Set("Content-Type", "application/xml")
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><DeleteResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">%s</DeleteResult>`, failed.String())
}

func writeS3Error(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>%s</Code><Message>%s</Message></Error>`, code, code)
}

func newTestClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	t.Setenv("AWS_CONFIG_FILE", "/nonexistent")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/nonexistent")
	c, err := New(context.Background(), &config.Config{
		S3Endpoint: endpoint, S3Region: "us-east-1", S3Bucket: "media",
		S3AccessKey: "test-access", S3SecretKey: "test-secret", S3ForcePathStyle: true,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return c
}

func TestNewCreatesMissingBucket(t *testing.T) {
	f, srv := newFakeS3(t)
	newTestClient(t, srv.URL)
	if !f.buckets["media"] {
		t.Fatal("bucket was not created")
	}
}

func (f *fakeS3) assertStored(t *testing.T, key string, data []byte, contentType string) {
	t.Helper()
	got := f.objects[key]
	if !bytes.Equal(got, data) {
		t.Fatalf("stored payload %q, want %q", got, data)
	}
	if f.types[key] != contentType {
		t.Fatalf("stored type %q, want %s", f.types[key], contentType)
	}
}

func assertOffsetGets(t *testing.T, ctx context.Context, c *Client, key string, data []byte, offsets []int64) {
	t.Helper()
	for _, offset := range offsets {
		rc, err := c.GetObjectFrom(ctx, key, offset)
		if err != nil {
			t.Fatalf("get from %d: %v", offset, err)
		}
		got, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read from offset %d: %v", offset, err)
		}
		if !bytes.Equal(got, data[offset:]) {
			t.Fatalf("get from %d: %q", offset, got)
		}
	}
}

func TestClientRoundTrip(t *testing.T) {
	f, srv := newFakeS3(t, "media")
	c := newTestClient(t, srv.URL)
	ctx := context.Background()

	data := []byte("0123456789")
	if err := c.Upload(ctx, "a/file.bin", bytes.NewReader(data), "application/octet-stream", int64(len(data))); err != nil {
		t.Fatalf("upload: %v", err)
	}
	f.assertStored(t, "media/a/file.bin", data, "application/octet-stream")

	assertOffsetGets(t, ctx, c, "a/file.bin", data, []int64{0, 4})
	if _, err := c.GetObjectFrom(ctx, "missing", 0); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing object: %v", err)
	}

	if err := c.Delete(ctx, "a/file.bin"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok := f.objects["media/a/file.bin"]; ok {
		t.Fatal("object still stored after delete")
	}
}

func assertSuccessfulBatchDelete(t *testing.T, ctx context.Context, c *Client, f *fakeS3, keys []string) {
	t.Helper()
	if err := c.DeleteBatch(ctx, keys); err != nil {
		t.Fatalf("delete batch: %v", err)
	}
	if f.batchDeletes != 3 {
		t.Fatalf("expected 3 batch deletes, got %d", f.batchDeletes)
	}
	if len(f.objects) != 0 {
		t.Fatalf("expected 0 objects left, got %d", len(f.objects))
	}
	if err := c.DeleteBatch(ctx, nil); err != nil {
		t.Fatalf("empty batch error: %v", err)
	}
	if f.batchDeletes != 3 {
		t.Fatalf("expected 3 batch requests, got %d", f.batchDeletes)
	}
}

func TestDeleteBatchChunksAndReportsFailures(t *testing.T) {
	f, srv := newFakeS3(t, "media")
	c := newTestClient(t, srv.URL)
	ctx := context.Background()

	keys := make([]string, 2500)
	for i := range keys {
		keys[i] = fmt.Sprintf("old/%d", i)
		f.objects["media/"+keys[i]] = []byte("x")
	}
	assertSuccessfulBatchDelete(t, ctx, c, f, keys)

	err := c.DeleteBatch(ctx, []string{"old/x", "locked/a", "locked/b"})
	if err == nil {
		t.Fatal("expected partial failure error")
	}
	if !strings.Contains(err.Error(), "2 of 3 failed") {
		t.Fatalf("partial failure not reported: %v", err)
	}
}

func TestClientReportsStorageErrors(t *testing.T) {
	f, srv := newFakeS3(t, "media")
	c := newTestClient(t, srv.URL)
	f.mu.Lock()
	f.deny = true
	f.mu.Unlock()
	ctx := context.Background()

	if err := c.Upload(ctx, "k", bytes.NewReader([]byte("x")), "text/plain", 1); err == nil || !strings.Contains(err.Error(), "put object k") {
		t.Fatalf("upload error: %v", err)
	}
	if err := c.Delete(ctx, "k"); err == nil || !strings.Contains(err.Error(), "delete object k") {
		t.Fatalf("delete error: %v", err)
	}
	if err := c.DeleteBatch(ctx, []string{"k"}); err == nil {
		t.Fatal("denied delete batch succeeded")
	}
	if _, err := New(ctx, &config.Config{S3Endpoint: srv.URL, S3Region: "us-east-1", S3Bucket: "media", S3AccessKey: "a", S3SecretKey: "b", S3ForcePathStyle: true}); err == nil {
		t.Fatal("New with denied bucket access succeeded")
	}
}
