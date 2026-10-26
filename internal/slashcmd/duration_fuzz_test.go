package slashcmd

import "testing"

// FuzzParseDuration checks the duration shorthand of the moderation commands
// never panics and never returns a negative span.
func FuzzParseDuration(f *testing.F) {
	for _, s := range []string{"", "30m", "2h", "7d", "2w", "90s", "0s", "9999999w", "99999999w", "-5m", "5 m", "5M", "1e3s", " 5m", "5m\n", "٣m", "\x00"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		n, err := parseDuration(s)
		if err != nil {
			if n != 0 {
				t.Fatalf("error with value %d", n)
			}
			return
		}
		if n < 0 {
			t.Fatalf("%q gave %d", s, n)
		}
	})
}
