package auth

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// RFC 6238 appendix B, SHA-1, secret "12345678901234567890". The RFC lists
// eight digits; six-digit codes are their last six.
func TestTOTPMatchesTheRFCVectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	for unix, want := range map[int64]string{
		59: "287082", 1111111109: "081804", 1111111111: "050471",
		1234567890: "005924", 2000000000: "279037", 20000000000: "353130",
	} {
		if got := totpCode(secret, unix/30); got != want {
			t.Errorf("t=%d: %s, want %s", unix, got, want)
		}
	}
}

func TestMatchTOTPAcceptsOneStepOfClockSkewOnly(t *testing.T) {
	secret := []byte("12345678901234567890")
	now := time.Unix(1111111109, 0)
	cur := totpStep(now)
	for _, tc := range []struct {
		step int64
		ok   bool
	}{{cur, true}, {cur - 1, true}, {cur + 1, true}, {cur - 2, false}, {cur + 2, false}} {
		step, ok := matchTOTP(secret, totpCode(secret, tc.step), now)
		if ok != tc.ok || (ok && step != tc.step) {
			t.Errorf("step offset %d: got (%d, %v)", tc.step-cur, step, ok)
		}
	}
	for _, bad := range []string{"", "12345", "1234567", "abcdef", " 123456"} {
		if _, ok := matchTOTP(secret, bad, now); ok {
			t.Errorf("%q must not match", bad)
		}
	}
}

func TestCodesAreNormalisedAndShapesTold(t *testing.T) {
	if got := cleanCode(" ABCD-efgh jkmn\t"); got != "abcdefghjkmn" {
		t.Fatalf("cleanCode: %q", got)
	}
	if !isTOTPShape("012345") || isTOTPShape("12345") || isTOTPShape("12345a") || isTOTPShape("1234567") {
		t.Fatal("isTOTPShape")
	}
}

func TestOtpauthURICarriesWhatAppsNeed(t *testing.T) {
	secret := []byte("12345678901234567890")
	u, err := url.Parse(otpauthURI(secret, "max", "Mnema Talk"))
	if err != nil || u.Scheme != "otpauth" || u.Host != "totp" {
		t.Fatalf("%v %v", u, err)
	}
	q := u.Query()
	if q.Get("secret") != "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ" || q.Get("issuer") != "Mnema Talk" ||
		q.Get("digits") != "6" || q.Get("period") != "30" || q.Get("algorithm") != "SHA1" {
		t.Fatalf("query %v", q)
	}
	if !strings.HasSuffix(u.Path, "Mnema Talk:max") {
		t.Fatalf("label %q", u.Path)
	}
}

func TestSecretsAreSealedPerAccount(t *testing.T) {
	s := &Sessions{Secret: strings.Repeat("s", 40)}
	a, b := uuid.New(), uuid.New()
	secret, err := newTOTPSecret()
	if err != nil || len(secret) != 20 {
		t.Fatalf("secret: %v %d", err, len(secret))
	}
	sealed, err := s.sealSecret(a, secret)
	if err != nil {
		t.Fatal(err)
	}
	if string(sealed) == string(secret) || strings.Contains(string(sealed), string(secret)) {
		t.Fatal("the secret must not be stored in the clear")
	}
	if got, err := s.openSecret(a, sealed); err != nil || string(got) != string(secret) {
		t.Fatalf("round trip: %v", err)
	}
	if _, err := s.openSecret(b, sealed); err == nil {
		t.Fatal("a secret copied to another account must not open")
	}
	if _, err := (&Sessions{Secret: strings.Repeat("t", 40)}).openSecret(a, sealed); err == nil {
		t.Fatal("another server secret must not open it")
	}
	if _, err := s.openSecret(a, sealed[:5]); err == nil {
		t.Fatal("truncated data")
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := s.openSecret(a, sealed); err == nil {
		t.Fatal("tampered data")
	}
}

func TestRecoveryCodesAreReadableAndKeyedPerAccount(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		c, err := newRecoveryCode()
		if err != nil || len(c) != recoveryCodeLen || strings.Trim(c, recoveryAlphabet) != "" || seen[c] {
			t.Fatalf("code %q: %v", c, err)
		}
		seen[c] = true
	}
	if got := formatRecoveryCode("abcdefghjkmn"); got != "abcd-efgh-jkmn" {
		t.Fatal(got)
	}
	s := &Sessions{Secret: strings.Repeat("s", 40)}
	a, b := uuid.New(), uuid.New()
	if string(s.recoveryHash(a, "abcdefghjkmn")) == string(s.recoveryHash(b, "abcdefghjkmn")) {
		t.Fatal("the hash must differ per account")
	}
	if string(s.recoveryHash(a, "abcdefghjkmn")) == string((&Sessions{Secret: "other-secret-other-secret-other-secret"}).recoveryHash(a, "abcdefghjkmn")) {
		t.Fatal("the hash must depend on the server secret")
	}
}
