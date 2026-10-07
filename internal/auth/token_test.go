package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const secret = "0123456789abcdef0123456789abcdef"

func TestTokenRoundTrip(t *testing.T) {
	id := uuid.New()
	tok, err := IssueToken(id, 3, secret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	c, err := ParseToken(tok, secret)
	if err != nil {
		t.Fatal(err)
	}
	if c.UserID != id || c.TokenVersion != 3 {
		t.Fatalf("claims %+v", c)
	}
}

func TestTokenRejections(t *testing.T) {
	id := uuid.New()
	valid, _ := IssueToken(id, 0, secret, time.Hour)
	expired, _ := IssueToken(id, 0, secret, -time.Minute)

	none := jwt.NewWithClaims(jwt.SigningMethodNone, Claims{UserID: id, RegisteredClaims: jwt.RegisteredClaims{
		Issuer: issuer, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}})
	noneTok, _ := none.SignedString(jwt.UnsafeAllowNoneSignatureType)

	foreign := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{UserID: id, RegisteredClaims: jwt.RegisteredClaims{
		Issuer: "someone-else", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}})
	foreignTok, _ := foreign.SignedString([]byte(secret))

	noExp := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{UserID: id, RegisteredClaims: jwt.RegisteredClaims{Issuer: issuer}})
	noExpTok, _ := noExp.SignedString([]byte(secret))

	cases := map[string]struct{ tok, secret string }{
		"wrong secret":   {valid, strings.Repeat("x", 32)},
		"expired":        {expired, secret},
		"alg none":       {noneTok, secret},
		"foreign issuer": {foreignTok, secret},
		"no expiry":      {noExpTok, secret},
		"garbage":        {"not.a.jwt", secret},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseToken(tc.tok, tc.secret); err == nil {
				t.Fatal("token accepted")
			}
		})
	}
}

func TestPasswordPolicy(t *testing.T) {
	if ValidatePassword("short") == nil {
		t.Error("short password accepted")
	}
	if ValidatePassword(strings.Repeat("a", 73)) == nil {
		t.Error("password beyond bcrypt's 72 bytes accepted (would be truncated)")
	}
	if err := ValidatePassword("correct horse battery"); err != nil {
		t.Errorf("good password rejected: %v", err)
	}
}

func TestUsernamePolicy(t *testing.T) {
	for _, bad := range []string{"ab", "has space", "<script>", strings.Repeat("a", 33), "ünï"} {
		if ValidateUsername(bad) == nil {
			t.Errorf("username %q accepted", bad)
		}
	}
	for _, good := range []string{"Herzog", "max_m", "a.b-c"} {
		if err := ValidateUsername(good); err != nil {
			t.Errorf("username %q rejected: %v", good, err)
		}
	}
}

func TestGenerateInviteCode(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		c, err := GenerateInviteCode()
		if err != nil {
			t.Fatal(err)
		}
		if !inviteCodePattern.MatchString(c) || seen[c] {
			t.Fatalf("bad or duplicate code %q", c)
		}
		seen[c] = true
	}
}

// Production's cost stays 12; only integration test binaries lower it once in
// TestMain. Check the timing-equaliser has the same cost in either binary.
func TestProductionPasswordCost(t *testing.T) {
	if passwordCost != expectedPasswordCost || bcryptCost != 12 {
		t.Fatalf("password cost %d (const %d), want %d (const 12)", passwordCost, bcryptCost, expectedPasswordCost)
	}
	if c, err := bcrypt.Cost(dummyHash); err != nil || c != expectedPasswordCost {
		t.Fatalf("timing-equaliser hash cost %d (%v), want %d", c, err, expectedPasswordCost)
	}
}
