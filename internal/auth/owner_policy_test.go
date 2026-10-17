package auth

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestOwnerPasswordPolicy(t *testing.T) {
	short, long := strings.Repeat("a", 12), strings.Repeat("a", 14)
	if err := ValidatePasswordFor(RoleAdmin, short); err == nil {
		t.Error("owner accepted 12 characters")
	}
	if err := ValidatePasswordFor(RoleAdmin, long); err != nil {
		t.Errorf("owner rejected 14 characters: %v", err)
	}
	if err := ValidatePasswordFor("user", short); err != nil {
		t.Errorf("member rejected 12 characters: %v", err)
	}
	if err := ValidateAdminSeed("Herzog", short); err == nil {
		t.Error("seeded owner password of 12 characters accepted")
	}
	if err := ValidateAdminSeed("Herzog", long); err != nil {
		t.Errorf("seeded owner password of 14 characters rejected: %v", err)
	}
	if err := ValidateAdminSeed("Herzog", ""); err != nil {
		t.Errorf("empty seed password (generated) rejected: %v", err)
	}
}

func TestSessionTokensCarryUniqueIDs(t *testing.T) {
	id := uuid.New()
	a, _ := IssueToken(id, 1, strings.Repeat("s", 32), time.Hour)
	b, _ := IssueToken(id, 1, strings.Repeat("s", 32), time.Hour)
	ca, err := ParseToken(a, strings.Repeat("s", 32))
	if err != nil {
		t.Fatal(err)
	}
	cb, _ := ParseToken(b, strings.Repeat("s", 32))
	if ca.ID == "" || ca.ID == cb.ID {
		t.Fatalf("session ids must be set and unique: %q %q", ca.ID, cb.ID)
	}
}

func TestPasswordHashingConcurrencyIsBounded(t *testing.T) {
	var running, peak atomic.Int32
	var wg sync.WaitGroup
	limit := cap(bcryptSlots)
	for i := 0; i < limit*3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release := acquireBcrypt()
			defer release()
			n := running.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			running.Add(-1)
		}()
	}
	wg.Wait()
	if int(peak.Load()) > limit {
		t.Fatalf("%d hashes ran at once, limit %d", peak.Load(), limit)
	}
}
