package s3

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strconv"
	"testing"

	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3svc "github.com/aws/aws-sdk-go-v2/service/s3"
)

// fakeObjects records what a streamed upload sends.
type fakeObjects struct {
	put       []byte
	parts     map[int32][]byte
	completed []int32
	aborted   bool
	failPart  int32
}

func (f *fakeObjects) PutObject(_ context.Context, in *s3svc.PutObjectInput, _ ...func(*s3svc.Options)) (*s3svc.PutObjectOutput, error) {
	b, _ := io.ReadAll(in.Body)
	if int64(len(b)) != aws.ToInt64(in.ContentLength) {
		return nil, errors.New("content length mismatch")
	}
	f.put = b
	return &s3svc.PutObjectOutput{}, nil
}

func (f *fakeObjects) CreateMultipartUpload(context.Context, *s3svc.CreateMultipartUploadInput, ...func(*s3svc.Options)) (*s3svc.CreateMultipartUploadOutput, error) {
	f.parts = map[int32][]byte{}
	return &s3svc.CreateMultipartUploadOutput{UploadId: aws.String("u1")}, nil
}

func (f *fakeObjects) UploadPart(_ context.Context, in *s3svc.UploadPartInput, _ ...func(*s3svc.Options)) (*s3svc.UploadPartOutput, error) {
	num := aws.ToInt32(in.PartNumber)
	if num == f.failPart {
		return nil, errors.New("part rejected")
	}
	b, _ := io.ReadAll(in.Body)
	if int64(len(b)) != aws.ToInt64(in.ContentLength) {
		return nil, errors.New("content length mismatch")
	}
	f.parts[num] = b
	return &s3svc.UploadPartOutput{ETag: aws.String("e" + strconv.Itoa(int(num)))}, nil
}

func (f *fakeObjects) CompleteMultipartUpload(_ context.Context, in *s3svc.CompleteMultipartUploadInput, _ ...func(*s3svc.Options)) (*s3svc.CompleteMultipartUploadOutput, error) {
	for _, p := range in.MultipartUpload.Parts {
		f.completed = append(f.completed, aws.ToInt32(p.PartNumber))
	}
	return &s3svc.CompleteMultipartUploadOutput{}, nil
}

func (f *fakeObjects) AbortMultipartUpload(context.Context, *s3svc.AbortMultipartUploadInput, ...func(*s3svc.Options)) (*s3svc.AbortMultipartUploadOutput, error) {
	f.aborted = true
	return &s3svc.AbortMultipartUploadOutput{}, nil
}

// assembled is the object S3 would hold after the upload.
func (f *fakeObjects) assembled() []byte {
	if f.parts == nil {
		return f.put
	}
	var out []byte
	for _, n := range f.completed {
		out = append(out, f.parts[n]...)
	}
	return out
}

func payload(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}

func TestUploadStreamSplitsIntoParts(t *testing.T) {
	for _, tc := range []struct {
		size  int
		parts int // 0 = single PutObject
	}{
		{100, 0},
		{partSize - 1, 0},
		{partSize, 1},
		{partSize + 1, 2},
		{2*partSize + 12345, 3},
	} {
		f := &fakeObjects{}
		data := payload(tc.size)
		if err := uploadStream(context.Background(), f, "b", "k", bytes.NewReader(data), "image/png"); err != nil {
			t.Fatalf("size %d: %v", tc.size, err)
		}
		if len(f.completed) != tc.parts {
			t.Errorf("size %d: %d parts, want %d", tc.size, len(f.completed), tc.parts)
		}
		if !bytes.Equal(f.assembled(), data) {
			t.Errorf("size %d: stored object differs from the upload", tc.size)
		}
		for num, p := range f.parts {
			if int(num) < len(f.parts) && len(p) != partSize {
				t.Errorf("size %d: part %d has %d bytes, every part but the last must be %d", tc.size, num, len(p), partSize)
			}
		}
		if f.aborted {
			t.Errorf("size %d: aborted a successful upload", tc.size)
		}
	}
}

func TestUploadStreamAbortsOnFailure(t *testing.T) {
	// The client's body breaks off after the first part (e.g. it exceeds
	// the size limit or disconnects).
	tooLarge := errors.New("file too large")
	body := io.MultiReader(bytes.NewReader(payload(partSize+10)), errReader{tooLarge})
	f := &fakeObjects{}
	if err := uploadStream(context.Background(), f, "b", "k", body, "video/mp4"); !errors.Is(err, tooLarge) {
		t.Fatalf("got %v, want the body's error", err)
	}
	if !f.aborted || f.completed != nil {
		t.Fatalf("aborted=%v completed=%v, want the multipart upload aborted", f.aborted, f.completed)
	}

	// S3 rejects a part.
	f = &fakeObjects{failPart: 2}
	if err := uploadStream(context.Background(), f, "b", "k", bytes.NewReader(payload(3*partSize)), "video/mp4"); err == nil {
		t.Fatal("a rejected part was not reported")
	}
	if !f.aborted || f.completed != nil {
		t.Fatalf("aborted=%v completed=%v, want the multipart upload aborted", f.aborted, f.completed)
	}
}

type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

func handleStreamUploadFailure(w http.ResponseWriter, r *http.Request, stage string, aborted *atomic.Bool) {
	if r.Method == http.MethodHead {
		return
	}
	q := r.URL.Query()
	switch {
	case r.Method == http.MethodDelete:
		aborted.Store(true)
		if stage == "abort" {
			writeS3Error(w, http.StatusForbidden, "AccessDenied")
		} else {
			w.WriteHeader(http.StatusNoContent)
		}
	case r.Method == http.MethodPost && q.Has("uploads"):
		if stage == "start" {
			writeS3Error(w, http.StatusForbidden, "AccessDenied")
			return
		}
		fmt.Fprint(w, `<InitiateMultipartUploadResult><UploadId>upload-test</UploadId></InitiateMultipartUploadResult>`)
	case r.Method == http.MethodPost && q.Has("uploadId"):
		writeS3Error(w, http.StatusForbidden, "AccessDenied")
	case r.Method == http.MethodPut && q.Has("partNumber"):
		if stage == "abort" {
			writeS3Error(w, http.StatusForbidden, "AccessDenied")
			return
		}
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("ETag", `"part-test"`)
	case r.Method == http.MethodPut:
		writeS3Error(w, http.StatusForbidden, "AccessDenied")
	default:
		http.Error(w, "unexpected request", http.StatusBadRequest)
	}
}

func TestStreamedUploadStorageFailures(t *testing.T) {
	for _, stage := range []string{"put", "start", "complete", "abort"} {
		t.Run(stage, func(t *testing.T) {
			var aborted atomic.Bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handleStreamUploadFailure(w, r, stage, &aborted)
			}))
			defer srv.Close()
			c := newTestClient(t, srv.URL)
			n := partSize + 1
			if stage == "put" {
				n = 10
			}
			err := c.Upload(context.Background(), "stream.bin", bytes.NewReader(payload(n)), "application/octet-stream", -1)
			want := map[string]string{"put": "put object", "start": "start multipart upload", "complete": "complete multipart upload", "abort": "upload part"}[stage]
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("failure at %s: %v", stage, err)
			}
			if aborted.Load() != (stage == "complete" || stage == "abort") {
				t.Fatalf("aborted=%v at %s", aborted.Load(), stage)
			}
		})
	}
}

func TestStreamedUploadInitialReadFailure(t *testing.T) {
	sentinel := errors.New("client disconnected")
	f := &fakeObjects{}
	if err := uploadStream(context.Background(), f, "media", "file", errReader{sentinel}, "text/plain"); !errors.Is(err, sentinel) {
		t.Fatalf("body failure: %v", err)
	}
	if f.put != nil || f.parts != nil || f.aborted {
		t.Fatal("initial body failure must not start storing an object")
	}
}
