package httpx

import (
	"testing"
	"time"
)

func TestCapLockoutLimitsEveryTier(t *testing.T) {
	l := NewLoginFailureLimiter(1, time.Minute) // tiers: 1, 5, 30 minutes
	l.CapLockout(15 * time.Minute)
	now := time.Now()
	l.now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		l.RecordFailure("k")
		locked, retry := l.IsLockedOut("k")
		if !locked || retry > 15*time.Minute {
			t.Fatalf("tier %d: locked=%v retry=%v", i+1, locked, retry)
		}
		now = now.Add(retry + time.Second)
	}
	l.CapLockout(time.Hour) // never raises a tier
	l.RecordFailure("k")
	if _, retry := l.IsLockedOut("k"); retry > 15*time.Minute {
		t.Fatalf("raised to %v", retry)
	}
}
