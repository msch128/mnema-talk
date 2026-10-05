package server

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTURNCredentialsVerifyWithTheSharedSecret(t *testing.T) {
	user := uuid.New()
	now := time.Unix(1_800_000_000, 0)
	name, cred := turnCredentials("s3cret-shared-with-coturn", user, now, 12*time.Hour)

	expiry, id, ok := strings.Cut(name, ":")
	if !ok || id != user.String() {
		t.Fatalf("username %q must be <expiry>:<user id>", name)
	}
	if exp, _ := strconv.ParseInt(expiry, 10, 64); exp != now.Add(12*time.Hour).Unix() {
		t.Fatalf("expiry %s", expiry)
	}
	// coturn's check: base64(HMAC-SHA1(secret, username)).
	mac := hmac.New(sha1.New, []byte("s3cret-shared-with-coturn"))
	mac.Write([]byte(name))
	if want := base64.StdEncoding.EncodeToString(mac.Sum(nil)); cred != want {
		t.Fatalf("credential %q, want %q", cred, want)
	}
}
