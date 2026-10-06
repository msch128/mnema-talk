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

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deny {
		writeS3Error(w, http.StatusForbidden, "AccessDenied")
		return
	}
	bucket, key, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if key == "" {
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
		return
	}
	if !f.buckets[bucket] {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket")
		return
	}
	id := bucket + "/" + key
	switch r.Method {
	case http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		f.objects[id] = body
		f.types[id] = r.Header.Get("Content-Type")
	case http.MethodDelete:
		delete(f.objects, id)
		w.WriteHeader(http.StatusNoContent)
	case http.MethodGet:
		body, ok := f.objects[id]
		if !ok {
			writeS3Error(w, http.StatusNotFound, "NoSuchKey")
			return
		}
		from, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(r.Header.Get("Range"), "bytes="), "-"))
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", from, len(body)-1, len(body)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(body[from:])
	default:
		w.WriteHeader(http.StatusNotImplemented)
	}
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

func TestClientRoundTrip(t *testing.T) {
	f, srv := newFakeS3(t, "media")
	c := newTestClient(t, srv.URL)
	ctx := context.Background()

	data := []byte("0123456789")
	if err := c.Upload(ctx, "a/file.bin", bytes.NewReader(data), "application/octet-stream", int64(len(data))); err != nil {
		t.Fatalf("upload: %v", err)
	}
	if got := f.objects["media/a/file.bin"]; !bytes.Equal(got, data) || f.types["media/a/file.bin"] != "application/octet-stream" {
		t.Fatalf("stored %q (%s)", got, f.types["media/a/file.bin"])
	}

	for _, offset := range []int64{0, 4} {
		rc, err := c.GetObjectFrom(ctx, "a/file.bin", offset)
		if err != nil {
			t.Fatalf("get from %d: %v", offset, err)
		}
		got, _ := io.ReadAll(rc)
		_ = rc.Close()
		if !bytes.Equal(got, data[offset:]) {
			t.Fatalf("get from %d: %q", offset, got)
		}
	}
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

func TestDeleteBatchChunksAndReportsFailures(t *testing.T) {
	f, srv := newFakeS3(t, "media")
	c := newTestClient(t, srv.URL)
	ctx := context.Background()

	keys := make([]string, 2500)
	for i := range keys {
		keys[i] = fmt.Sprintf("old/%d", i)
		f.objects["media/"+keys[i]] = []byte("x")
	}
	if err := c.DeleteBatch(ctx, keys); err != nil {
		t.Fatalf("delete batch: %v", err)
	}
	if f.batchDeletes != 3 || len(f.objects) != 0 {
		t.Fatalf("%d requests, %d objects left; want 3 requests and none left", f.batchDeletes, len(f.objects))
	}
	if err := c.DeleteBatch(ctx, nil); err != nil || f.batchDeletes != 3 {
		t.Fatalf("empty batch: %v, %d requests", err, f.batchDeletes)
	}

	err := c.DeleteBatch(ctx, []string{"old/x", "locked/a", "locked/b"})
	if err == nil || !strings.Contains(err.Error(), "2 of 3 failed") {
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
