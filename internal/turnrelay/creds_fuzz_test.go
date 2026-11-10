package turnrelay

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// FuzzParseUsername checks the TURN username parser never panics, refuses
// with one error and no partial result, and accepts only "<expiry>:<member
// id>" with an expiry after the epoch, which reads back the same when it is
// written the way mint writes it.
func FuzzParseUsername(f *testing.F) {
	const id = "0b5c1e7a-52d1-4c53-9d3a-8f0e6f1f2a44"
	for _, s := range []string{
		"1800003600:" + id, "", "x", "1:2", "abc:" + id, "0:" + id, "-5:" + id, "+7:" + id,
		"9223372036854775807:" + id, "9223372036854775808:" + id, "1800003600:" + id + ":x",
		"1800003600:" + strings.ReplaceAll(id, "-", ""), "1800003600:{" + id + "}", "1800003600:urn:uuid:" + id, ":" + id, "1800003600:",
	} {
		f.Add(s)
	}
	a, err := newAuthority()
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, username string) {
		user, expires, err := parseUsername(username)
		if err != nil {
			if err != errMalformed || user != uuid.Nil || !expires.IsZero() {
				t.Fatalf("parseUsername(%q) = %v %v %v", username, user, expires, err)
			}
			return
		}
		if !strings.Contains(username, ":") || expires.Unix() <= 0 {
			t.Fatalf("parseUsername(%q) accepted with expiry %v", username, expires)
		}
		canonical := strconv.FormatInt(expires.Unix(), 10) + ":" + user.String()
		again, exp, err := parseUsername(canonical)
		if err != nil || again != user || !exp.Equal(expires) {
			t.Fatalf("%q read as %q, which reads back as %v %v %v", username, canonical, again, exp, err)
		}
		if _, _, err := a.check(username, expires); err != errExpired {
			t.Fatalf("check(%q) at its expiry: %v, want expired", username, err)
		}
		if got, _, err := a.check(username, expires.Add(-time.Second)); err != nil || got != user {
			t.Fatalf("check(%q) a second before its expiry: %v %v", username, got, err)
		}
	})
}
