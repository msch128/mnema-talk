package auth

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

// One hostile address hammering a locked account must not use up what a
// legitimate new device needs (AUTH-01).
func TestHostileClientCannotStarveNewDeviceSignIn(t *testing.T) {
	h := newLoginHandler()
	tripAccountCap(t, h)

	evaluated := 0
	inner := h.verify
	h.verify = func(ctx context.Context, u, p string) (*User, int, error) { evaluated++; return inner(ctx, u, p) }

	for i := 0; i < 200; i++ {
		postLoginWith(h, "203.0.113.9", "cool_guy", "guess")
	}
	if evaluated > 2 {
		t.Fatalf("one address got %d password checks, want at most its burst of 2", evaluated)
	}
	if code := postLoginWith(h, "192.0.2.77", "cool_guy", testPassword).Code; code != http.StatusOK {
		t.Fatalf("owner on a new device while one address hammers: got %d, want 200", code)
	}
}

// Refused requests are not charged to the account-wide ceiling.
func TestRefusedUnknownDeviceRequestsDoNotSpendAccountCeiling(t *testing.T) {
	h := newLoginHandler()
	tripAccountCap(t, h)
	for i := 0; i < 500; i++ {
		postLoginWith(h, "203.0.113.9", "cool_guy", "guess")
	}
	// Only the hostile address's 2 granted checks were charged, so many other
	// clients still get through.
	for i := 0; i < 9; i++ {
		ip := fmt.Sprintf("192.0.2.%d", 100+i)
		if code := postLoginWith(h, ip, "cool_guy", testPassword).Code; code != http.StatusOK {
			t.Fatalf("client %d: got %d, want 200", i, code)
		}
	}
}

// The account-wide ceiling still bounds guessing from many addresses.
func TestAccountCeilingBoundsDistributedGuessing(t *testing.T) {
	h := newLoginHandler()
	tripAccountCap(t, h)
	evaluated := 0
	inner := h.verify
	h.verify = func(ctx context.Context, u, p string) (*User, int, error) { evaluated++; return inner(ctx, u, p) }
	for i := 0; i < 300; i++ {
		postLoginWith(h, fmt.Sprintf("198.18.%d.%d", i/200, i%200), "cool_guy", "guess")
	}
	if evaluated != 20 {
		t.Fatalf("evaluated %d, want the ceiling of 20", evaluated)
	}
}
