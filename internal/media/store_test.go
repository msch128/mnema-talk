package media

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func TestMemoryStoreDoesNotKeepFailedUploadAndStopsListing(t *testing.T) {
	s := NewMemoryStore()
	sentinel := errors.New("broken source")
	if err := s.Upload(context.Background(), "failed", failingReader{sentinel}, "text/plain", -1); !errors.Is(err, sentinel) {
		t.Fatalf("upload failure: %v", err)
	}
	if s.Has("failed") {
		t.Fatal("failed upload was stored")
	}
	if err := s.Upload(context.Background(), "key", strings.NewReader("abc"), "text/plain", 3); err != nil {
		t.Fatal(err)
	}
	body, err := s.GetObjectFrom(context.Background(), "key", 99)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil || len(data) != 0 {
		t.Fatalf("past-end body=%q %v", data, err)
	}
	if err := s.List(context.Background(), "", func(ObjectInfo) error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatalf("listing callback: %v", err)
	}
}
