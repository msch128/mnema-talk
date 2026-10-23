package auth

import (
	"strings"
	"unicode"

	"github.com/msch128/mnema-talk/internal/httpx"
)

// Custom invite codes are typed by an admin, so unlike the generated ones
// (about 59 bits) they can be as guessable as "welcome". Anyone can test a
// code against the public preview, so new custom codes must clear a floor.
// Codes created before the floor existed no longer work for sign-up (preview
// and registration treat them as unknown); the admin API flags them
// (Invite.Weak) so they can be replaced.
const (
	minCustomCodeLength   = 12
	minCustomCodeDistinct = 7
	// longCustomCode is the length from which a code needs no digit.
	longCustomCode = 16
)

// weakCodeWords are what an attacker tries first. A code is weak when one of
// them makes up all of it but a few characters ("welcome2026"); inside a long
// phrase it does no harm.
var weakCodeWords = []string{
	"welcome", "invite", "invitation", "mnema", "password", "passwort",
	"secret", "letmein", "admin", "qwert", "asdf", "12345", "abcde",
}

// weakCodeSlack is how many other characters a code needs next to a weak word.
const weakCodeSlack = 6

// inviteCodeWeakness names why code is easy to guess, or "" when it is fine.
func inviteCodeWeakness(code string) string {
	if len(code) < minCustomCodeLength {
		return "shorter than 12 characters"
	}
	distinct := map[rune]struct{}{}
	var letters, digits bool
	for _, r := range strings.ToLower(code) {
		distinct[r] = struct{}{}
		letters = letters || unicode.IsLetter(r)
		digits = digits || unicode.IsDigit(r)
	}
	if len(distinct) < minCustomCodeDistinct {
		return "too repetitive"
	}
	if len(code) < longCustomCode && !(letters && digits) {
		return "needs letters and digits, or 16 or more characters"
	}
	lower := strings.ToLower(code)
	for _, w := range weakCodeWords {
		if strings.Contains(lower, w) && len(lower)-len(w) < weakCodeSlack {
			return "contains a common word or sequence"
		}
	}
	return ""
}

// IsWeakInviteCode reports whether code would not be accepted as a new custom
// code today.
func IsWeakInviteCode(code string) bool { return inviteCodeWeakness(code) != "" }

func checkCustomCodeStrength(code string) error {
	if why := inviteCodeWeakness(code); why != "" {
		return httpx.ErrInvalidInput("code is too easy to guess (" + why + "); leave it empty for a random code or use at least 12 hard-to-guess characters")
	}
	return nil
}
