package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 authenticator apps use HMAC-SHA1
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP as RFC 6238 defines it, in the profile every authenticator app
// supports: HMAC-SHA1, 6 digits, 30 second steps.
const (
	totpDigits     = 6
	totpPeriod     = 30
	totpSecretLen  = 20
	totpSkewSteps  = 1
	totpIssuerName = "Mnema Talk"
)

var totpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// newTOTPSecret returns 160 random bits.
func newTOTPSecret() ([]byte, error) {
	b := make([]byte, totpSecretLen)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("random secret: %w", err)
	}
	return b, nil
}

// totpStep is the 30 second counter of t.
func totpStep(t time.Time) int64 { return t.Unix() / totpPeriod }

// totpCode is the code for one step (RFC 4226 dynamic truncation).
func totpCode(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	m := hmac.New(sha1.New, secret)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	n := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, n%1_000_000)
}

// matchTOTP returns the step of the first code in the skew window that equals
// input, comparing every candidate in constant time. ok is false when none
// matches. Which steps are acceptable beyond that (replay) is the caller's.
func matchTOTP(secret []byte, input string, now time.Time) (step int64, ok bool) {
	if len(input) != totpDigits {
		return 0, false
	}
	cur := totpStep(now)
	for s := cur - totpSkewSteps; s <= cur+totpSkewSteps; s++ {
		if subtle.ConstantTimeCompare([]byte(totpCode(secret, s)), []byte(input)) == 1 && !ok {
			step, ok = s, true
		}
	}
	return step, ok
}

// otpauthURI is what authenticator apps read from the QR code.
func otpauthURI(secret []byte, username string) string {
	label := url.PathEscape(totpIssuerName + ":" + username)
	q := url.Values{}
	q.Set("secret", totpEncoding.EncodeToString(secret))
	q.Set("issuer", totpIssuerName)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(totpDigits))
	q.Set("period", fmt.Sprint(totpPeriod))
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// cleanCode removes the spaces and dashes people type or copy into a code.
func cleanCode(s string) string {
	return strings.ToLower(strings.NewReplacer(" ", "", "-", "", "\t", "").Replace(strings.TrimSpace(s)))
}

// isTOTPShape reports whether s looks like an authenticator code (six digits).
func isTOTPShape(s string) bool {
	if len(s) != totpDigits {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
