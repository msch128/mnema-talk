package chat

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/google/uuid"
)

func TestInflightCapsPerKeyAndReleases(t *testing.T) {
	f := &inflight{max: 2, n: map[uuid.UUID]int{}}
	a, b := uuid.New(), uuid.New()
	r1, ok1 := f.acquire(a)
	r2, ok2 := f.acquire(a)
	if !ok1 || !ok2 {
		t.Fatal("first two slots must be granted")
	}
	if _, ok := f.acquire(a); ok {
		t.Fatal("third slot granted")
	}
	if _, ok := f.acquire(b); !ok {
		t.Fatal("another key must not be affected")
	}
	r1()
	if _, ok := f.acquire(a); !ok {
		t.Fatal("slot not freed")
	}
	r2()
	if len(f.n) != 2 { // a (re-acquired) and b still hold slots
		t.Fatalf("bookkeeping: %v", f.n)
	}
}

func TestMapSearchError(t *testing.T) {
	if mapSearchError(nil) != nil {
		t.Fatal("nil stays nil")
	}
	if got := mapSearchError(&pgconn.PgError{Code: "57014"}); got != errSearchSlow {
		t.Fatalf("timeout mapped to %v", got)
	}
	boom := errors.New("boom")
	if got := mapSearchError(boom); !errors.Is(got, boom) || got == error(errSearchSlow) {
		t.Fatalf("other errors must be wrapped: %v", got)
	}
}
