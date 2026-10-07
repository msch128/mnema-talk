package media

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestObjectReaderLazyReadAndSeekValidation(t *testing.T) {
	store := NewMemoryStore()
	if err := store.Upload(context.Background(), "key", strings.NewReader("012345"), "text/plain", 6); err != nil {
		t.Fatal(err)
	}
	o := &objectReader{ctx: context.Background(), store: store, key: "key", size: 6}
	defer o.Close()
	if offset, err := o.Seek(2, io.SeekCurrent); err != nil || offset != 2 {
		t.Fatalf("relative seek=%d %v", offset, err)
	}
	data := make([]byte, 1)
	if n, err := o.Read(data); err != nil || n != 1 || string(data) != "2" {
		t.Fatalf("lazy read=%q n=%d err=%v", data, n, err)
	}
	if _, err := o.Seek(0, 100); err == nil {
		t.Fatal("invalid whence accepted")
	}
	if _, err := o.Seek(-7, io.SeekEnd); err == nil {
		t.Fatal("negative seek accepted")
	}
	if o.off != 3 {
		t.Fatal("invalid seek modified the offset")
	}
	if _, err := o.Seek(-1, io.SeekEnd); err != nil || o.body != nil {
		t.Fatalf("seek end did not release old body: %v", err)
	}
	if n, err := o.Read(data); n != 1 || err != nil || string(data) != "5" {
		t.Fatalf("reread=%q n=%d error=%v", data, n, err)
	}
	if _, err := o.Read(data); !errors.Is(err, io.EOF) {
		t.Fatalf("at EOF: %v", err)
	}
	missing := &objectReader{ctx: context.Background(), store: store, key: "missing", size: 1}
	if _, err := missing.Read(data); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("lazy open failure: %v", err)
	}
}

func TestRangeStartMalformedAndZeroSuffix(t *testing.T) {
	for _, header := range []string{"bytes=10", "bytes=-0", "bytes=-abc", "bytes=-1-", "bytes=-5", "bytes=100-"} {
		if got := rangeStart(header, 5); got != 0 {
			t.Fatalf("range %q starts at %d", header, got)
		}
	}
}

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

func TestStorageExtensionsAndDisallowedConcreteType(t *testing.T) {
	for mime, want := range map[string]string{"image/jpeg": ".jpg", "image/svg+xml": ".svg", "audio/mpeg": ".mp3", "audio/wave": ".wav", "text/plain": ".txt", "text/markdown": ".md", "application/x-mnema-unknown": ".bin"} {
		if got := extensionFor(mime); got != want {
			t.Fatalf("extension for %s = %s, want %s", mime, got, want)
		}
		if key := storageKey("uploads", uuid.New(), mime); !strings.HasPrefix(key, "uploads/") || !strings.HasSuffix(key, want) {
			t.Fatalf("storage key=%s", key)
		}
	}
	if _, err := reconcileMIME("image/png", pngBytes, map[string]bool{"text/plain": true}, false); err == nil {
		t.Fatal("concrete sniff bypassed allow-list")
	}
}
