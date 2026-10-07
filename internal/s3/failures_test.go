package s3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/smithy-go"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/media"
)

func TestNewReportsInvalidAWSConfiguration(t *testing.T) {
	t.Setenv("AWS_RETRY_MODE", "invalid-test-mode")
	_, err := New(context.Background(), &config.Config{S3Region: "us-east-1"})
	if err == nil || !strings.Contains(err.Error(), "load s3 config") {
		t.Fatalf("malformed configuration: %v", err)
	}
}

func TestIsNotFoundByServiceCode(t *testing.T) {
	for _, code := range []string{"NotFound", "NoSuchBucket", "AccessDenied"} {
		err := fmt.Errorf("wrapped: %w", &smithy.GenericAPIError{Code: code})
		if got := isNotFound(err); got != (code != "AccessDenied") {
			t.Fatalf("code %s: missing=%v", code, got)
		}
	}
}

func TestListObjectsPaginatesAndPropagatesErrors(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			return
		}
		if r.URL.Query().Get("prefix") != "uploads/" || r.URL.Query().Get("list-type") != "2" {
			t.Errorf("list query: %s", r.URL.RawQuery)
		}
		requests.Add(1)
		w.Header().Set("Content-Type", "application/xml")
		if r.URL.Query().Get("continuation-token") == "page2" {
			fmt.Fprint(w, `<ListBucketResult><IsTruncated>false</IsTruncated><Contents><Key>uploads/b</Key><Size>8</Size><LastModified>2020-01-02T03:04:05Z</LastModified></Contents></ListBucketResult>`)
			return
		}
		fmt.Fprint(w, `<ListBucketResult><IsTruncated>true</IsTruncated><NextContinuationToken>page2</NextContinuationToken><Contents><Key>uploads/a</Key><Size>7</Size><LastModified>2020-01-02T03:04:05Z</LastModified></Contents></ListBucketResult>`)
	}))
	defer srv.Close()
	c := newTestClient(t, srv.URL)
	var objects []media.ObjectInfo
	if err := c.List(context.Background(), "uploads/", func(o media.ObjectInfo) error { objects = append(objects, o); return nil }); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 || len(objects) != 2 || objects[0].Key != "uploads/a" || objects[1].Size != 8 || !objects[0].LastModified.Equal(time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("pages=%d, objects=%+v", requests.Load(), objects)
	}
	sentinel := errors.New("stop listing")
	if err := c.List(context.Background(), "uploads/", func(media.ObjectInfo) error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatalf("callback error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.List(ctx, "uploads/", func(media.ObjectInfo) error { t.Fatal("callback after canceled list"); return nil }); !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "list objects uploads/") {
		t.Fatalf("canceled list: %v", err)
	}
}

func TestStreamedUploadStorageFailures(t *testing.T) {
	for _, stage := range []string{"put", "start", "complete", "abort"} {
		t.Run(stage, func(t *testing.T) {
			var aborted atomic.Bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
					_, _ = io.Copy(io.Discard, r.Body)
					w.Header().Set("ETag", `"part-test"`)
				case r.Method == http.MethodPut:
					writeS3Error(w, http.StatusForbidden, "AccessDenied")
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
				}
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
