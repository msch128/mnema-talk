package chat

import (
	"testing"
	"time"
)

// FuzzParseSearchDay checks the "during" day and its time zone: never a panic,
// a refused input gives no window, and an accepted one is a positive window of
// about one day (23 to 25 hours around a daylight saving change).
func FuzzParseSearchDay(f *testing.F) {
	for _, s := range [][2]string{
		{"2026-03-04", ""}, {"2026-03-04", "utc"}, {"2026-03-04", "+02:00"}, {"2026-03-04", "-0530"}, {"2026-03-04", "120"},
		{"2026-03-04", "-90"}, {"2026-03-29", "Europe/Berlin"}, {"2026-03-04", "../etc/passwd"}, {"2026-03-04", "/etc/localtime"},
		{"2026-02-30", "Z"}, {"0000-01-01", "+14:00"}, {"9999-12-31", "-14:00"}, {"", "x"}, {"2026-03-04", "99999999999999999999"}, {"2026-03-04", "\x00"},
	} {
		f.Add(s[0], s[1])
	}
	f.Fuzz(func(t *testing.T, during, tz string) {
		start, end, err := ParseSearchDay(during, tz)
		if err != nil {
			if start != nil || end != nil {
				t.Fatal("an error came with a window")
			}
			return
		}
		if during == "" {
			if start != nil || end != nil {
				t.Fatal("no day, but a window")
			}
			return
		}
		if start == nil || end == nil {
			t.Fatalf("missing window for %q %q", during, tz)
		}
		if d := end.Sub(*start); d < 23*time.Hour || d > 25*time.Hour {
			t.Fatalf("window of %v for %q %q", d, during, tz)
		}
	})
}

// FuzzSlowModeLimit checks the slow-mode value is accepted exactly from off
// (0) to the 24 hour limit, for every integer a client can send.
func FuzzSlowModeLimit(f *testing.F) {
	for _, n := range []int{0, 1, 5, 3600, MaxSlowModeSeconds, MaxSlowModeSeconds + 1, -1, 1 << 62, -(1 << 62)} {
		f.Add(n)
	}
	f.Fuzz(func(t *testing.T, n int) {
		err := validateChannelLimits(nil, &n)
		if ok := n >= 0 && n <= MaxSlowModeSeconds; ok != (err == nil) {
			t.Fatalf("slow mode %d: error %v", n, err)
		}
	})
}
