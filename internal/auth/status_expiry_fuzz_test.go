package auth

import (
	"testing"
	"time"
)

// FuzzStatusExpiry checks a status expiry is either refused or becomes a UTC
// whole-second time in the future and within the allowed span, and that an
// empty status never keeps an expiry.
func FuzzStatusExpiry(f *testing.F) {
	for _, s := range []struct {
		text   string
		offset int64
	}{
		{"away", 3600}, {"away", -1}, {"away", 0}, {"", 3600}, {"x", int64(MaxStatusExpiry / time.Second)}, {"x", int64(MaxStatusExpiry/time.Second) + 1},
		{"x", 1 << 40}, {"x", -(1 << 40)}, {"x", 1},
	} {
		f.Add(s.text, s.offset)
	}
	f.Fuzz(func(t *testing.T, text string, offsetSeconds int64) {
		if offsetSeconds > 1<<40 || offsetSeconds < -(1<<40) {
			return
		}
		at := time.Now().Add(time.Duration(offsetSeconds) * time.Second)
		got, err := checkStatusExpiry(text, &at)
		if err != nil || got == nil {
			return
		}
		now := time.Now().Add(time.Second)
		if text == "" || got.Location() != time.UTC || got.Nanosecond() != 0 || !got.After(now.Add(-2*time.Second)) || got.After(now.Add(MaxStatusExpiry)) {
			t.Fatalf("accepted %q at %v as %v", text, at, got)
		}
	})
}
