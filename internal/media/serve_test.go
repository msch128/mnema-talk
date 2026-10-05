package media

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func TestRangeStart(t *testing.T) {
	const size = 1000
	for header, want := range map[string]int64{
		"":                  0,
		"bytes=0-":          0,
		"bytes=100-199":     100,
		"bytes=500-":        500,
		"bytes=-200":        800,
		"bytes=-5000":       0,
		"bytes=1000-":       0, // past the end: ServeContent answers 416
		"bytes=0-1,5-9":     0,
		"items=5-":          0,
		"bytes=x-":          0,
		" bytes= 300 - 400": 300,
	} {
		if got := rangeStart(header, size); got != want {
			t.Errorf("rangeStart(%q) = %d, want %d", header, got, want)
		}
	}
}

func TestIsMissingObject(t *testing.T) {
	for _, err := range []error{
		ErrObjectNotFound,
		fmt.Errorf("get object k from 0: %w", &types.NoSuchKey{}),
		fmt.Errorf("wrapped: %w", &types.NotFound{}),
	} {
		if !isMissingObject(err) {
			t.Errorf("%v not recognised as a missing object", err)
		}
	}
	if isMissingObject(io.ErrUnexpectedEOF) {
		t.Error("an I/O error is not a missing object")
	}
}

// open fetches eagerly, and a following Seek to the same offset (what
// http.ServeContent does) reuses that body.
func TestObjectReaderOpen(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	if err := store.Upload(ctx, "k", strings.NewReader("0123456789"), "text/plain", 10); err != nil {
		t.Fatal(err)
	}
	o := &objectReader{ctx: ctx, store: store, key: "k", size: 10}
	if err := o.open(4); err != nil {
		t.Fatal(err)
	}
	body := o.body
	if _, err := o.Seek(4, io.SeekStart); err != nil || o.body != body {
		t.Fatal("seek to the opened offset dropped the body")
	}
	rest, err := io.ReadAll(o)
	if err != nil || string(rest) != "456789" {
		t.Fatalf("read %q, %v", rest, err)
	}

	missing := &objectReader{ctx: ctx, store: store, key: "gone", size: 10}
	if err := missing.open(0); !isMissingObject(err) {
		t.Fatalf("open of a missing object = %v", err)
	}
}

// Retention runs soon after start, not only after the first full interval.
func TestRunPeriodicallyRunsSoonThenRepeats(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var runs atomic.Int32
	go runPeriodically(ctx, 5*time.Millisecond, 20*time.Millisecond, func(context.Context) { runs.Add(1) })
	deadline := time.Now().Add(2 * time.Second)
	for runs.Load() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("ran %d times", runs.Load())
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
}
