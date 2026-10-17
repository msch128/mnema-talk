package httpx

import (
	"strings"
	"testing"
	"time"
)

func TestLoginFailureLimiterClearAndLockedKeys(t *testing.T) {
	l := NewLoginFailureLimiter(2, time.Minute)
	l.RecordFailures("a|x", 2)
	l.RecordFailure("b|x")
	l.RecordFailures("c|y", 2)
	if keys := l.LockedKeys(); len(keys) != 2 {
		t.Fatalf("locked keys = %v", keys)
	}
	if n := l.Clear(func(k string) bool { return strings.HasSuffix(k, "|x") }); n != 2 {
		t.Fatalf("cleared %d", n)
	}
	if keys := l.LockedKeys(); len(keys) != 1 || keys[0] != "c|y" {
		t.Fatalf("locked keys after clear = %v", keys)
	}
	if l.Len() != 1 {
		t.Fatalf("len = %d", l.Len())
	}
}

func TestRateLimiterClear(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)
	rl.Allow("a")
	rl.Allow("b")
	if n := rl.Clear(func(k string) bool { return k == "a" }); n != 1 {
		t.Fatalf("cleared %d", n)
	}
	if ok, _ := rl.Allow("a"); !ok {
		t.Fatal("cleared key still limited")
	}
	if ok, _ := rl.Allow("b"); ok {
		t.Fatal("other key was cleared")
	}
}
