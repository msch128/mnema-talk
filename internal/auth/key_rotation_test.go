package auth

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

var (
	sessionSecretA = strings.Repeat("a", 40)
	sessionSecretB = strings.Repeat("b", 40)
	dataKeyOne     = strings.Repeat("1", 40)
	dataKeyTwo     = strings.Repeat("2", 40)
)

// sealAsReleasedBeforeKeyIDs is the sealing code of every release before key
// ids, kept here on its own so the stored format cannot drift unnoticed.
func sealAsReleasedBeforeKeyIDs(t *testing.T, jwtSecret string, userID uuid.UUID, secret []byte) []byte {
	t.Helper()
	m := hmac.New(sha256.New, []byte(jwtSecret))
	m.Write([]byte("mnema-two-factor-totp-secret-v1"))
	block, err := aes.NewCipher(m.Sum(nil))
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	return aead.Seal(nonce, nonce, secret, userID[:])
}

func TestAnInstanceWithoutDataKeyStoresWhatItAlwaysDid(t *testing.T) {
	s := &Sessions{Secret: sessionSecretA}
	id, secret := uuid.New(), []byte("twenty-byte-secret-!")

	old := sealAsReleasedBeforeKeyIDs(t, sessionSecretA, id, secret)
	got, stale, err := s.openSecret(id, old)
	if err != nil || stale || !bytes.Equal(got, secret) {
		t.Fatalf("a secret sealed by an earlier release: %q stale=%v %v", got, stale, err)
	}

	sealed, err := s.sealSecret(id, secret)
	if err != nil {
		t.Fatal(err)
	}
	if len(sealed) != len(old) {
		t.Fatalf("the stored format changed: %d bytes, was %d", len(sealed), len(old))
	}
	// An earlier release reads it back: nonce, then ciphertext, account id as associated data.
	m := hmac.New(sha256.New, []byte(sessionSecretA))
	m.Write([]byte("mnema-two-factor-totp-secret-v1"))
	block, _ := aes.NewCipher(m.Sum(nil))
	aead, _ := cipher.NewGCM(block)
	if plain, err := aead.Open(nil, sealed[:12], sealed[12:], id[:]); err != nil || !bytes.Equal(plain, secret) {
		t.Fatalf("an earlier release could not read it: %v", err)
	}

	// The recovery code hash is the same keyed hash as before.
	rk := hmac.New(sha256.New, []byte(sessionSecretA))
	rk.Write([]byte("mnema-two-factor-recovery-code-v1"))
	h := hmac.New(sha256.New, rk.Sum(nil))
	h.Write(id[:])
	h.Write([]byte("abcdefghjkmn"))
	if !bytes.Equal(s.recoveryHash(id, "abcdefghjkmn"), h.Sum(nil)) {
		t.Fatal("the recovery code hash changed")
	}
}

func TestSecretsUnderADataKeyNameTheirKey(t *testing.T) {
	id, secret := uuid.New(), []byte("twenty-byte-secret-!")
	s := &Sessions{Secret: sessionSecretA, DataKey: dataKeyOne}
	sealed, err := s.sealSecret(id, secret)
	if err != nil {
		t.Fatal(err)
	}
	keyID, _, ok := parseSealed(sealed)
	if !ok || keyID == 0 || keyID != dataKeyID(dataKeyOne) || keyID == dataKeyID(dataKeyTwo) {
		t.Fatalf("key id in front of the blob: %d ok=%v", keyID, ok)
	}
	if got, stale, err := s.openSecret(id, sealed); err != nil || stale || !bytes.Equal(got, secret) {
		t.Fatalf("round trip: stale=%v %v", stale, err)
	}
	if _, _, err := s.openSecret(uuid.New(), sealed); err == nil {
		t.Fatal("a secret copied to another account must not open")
	}
	// The session secret no longer matters to it.
	if _, _, err := (&Sessions{Secret: sessionSecretB, DataKey: dataKeyOne}).openSecret(id, sealed); err != nil {
		t.Fatalf("after rotating the session secret: %v", err)
	}
}

func TestSecretsUnderAPreviousKeyOpenAndAreMarkedStale(t *testing.T) {
	id, secret := uuid.New(), []byte("twenty-byte-secret-!")
	underSession, _ := (&Sessions{Secret: sessionSecretA}).sealSecret(id, secret)
	underOne, _ := (&Sessions{Secret: sessionSecretA, DataKey: dataKeyOne}).sealSecret(id, secret)

	cases := []struct {
		name   string
		s      *Sessions
		sealed []byte
	}{
		{"moving from the session secret to a data key", &Sessions{Secret: sessionSecretA, DataKey: dataKeyOne}, underSession},
		{"a rotated data key", &Sessions{Secret: sessionSecretA, DataKey: dataKeyTwo, DataKeyPrevious: dataKeyOne}, underOne},
		{"a rotated session secret without a data key", &Sessions{Secret: sessionSecretB, PreviousSecret: sessionSecretA}, underSession},
		{"a data key while the session secret rotates", &Sessions{Secret: sessionSecretB, PreviousSecret: sessionSecretA, DataKey: dataKeyOne}, underSession},
	}
	for _, tc := range cases {
		got, stale, err := tc.s.openSecret(id, tc.sealed)
		if err != nil || !stale || !bytes.Equal(got, secret) {
			t.Errorf("%s: stale=%v %v", tc.name, stale, err)
		}
	}
}

func TestUnreadableBlobsAreRefused(t *testing.T) {
	id, secret := uuid.New(), []byte("twenty-byte-secret-!")
	underOne, _ := (&Sessions{Secret: sessionSecretA, DataKey: dataKeyOne}).sealSecret(id, secret)
	underSession, _ := (&Sessions{Secret: sessionSecretA}).sealSecret(id, secret)
	s := &Sessions{Secret: sessionSecretA, DataKey: dataKeyTwo}

	if _, _, err := s.openSecret(id, underOne); err != errUnreadableSecret {
		t.Fatalf("a blob with an unknown key id: %v", err)
	}
	if _, _, err := (&Sessions{Secret: sessionSecretB}).openSecret(id, underSession); err != errUnreadableSecret {
		t.Fatalf("a blob under a session secret that is gone: %v", err)
	}
	// The right key id in front of another key's ciphertext.
	forged := append((dataKey{root: dataKeyTwo, id: dataKeyID(dataKeyTwo)}).header(), underOne[sealedHeaderLen:]...)
	if _, _, err := s.openSecret(id, forged); err != errUnreadableSecret {
		t.Fatalf("a relabelled blob: %v", err)
	}
	own, _ := s.sealSecret(id, secret)
	for n := 0; n < len(own); n++ {
		if _, _, err := s.openSecret(id, own[:n]); err != errUnreadableSecret {
			t.Fatalf("a blob truncated to %d bytes: %v", n, err)
		}
	}
	for n := 0; n < len(underSession); n++ {
		if _, _, err := (&Sessions{Secret: sessionSecretA}).openSecret(id, underSession[:n]); err != errUnreadableSecret {
			t.Fatalf("an unprefixed blob truncated to %d bytes: %v", n, err)
		}
	}
	own[len(own)-1] ^= 1
	if _, _, err := s.openSecret(id, own); err != errUnreadableSecret {
		t.Fatalf("a tampered blob: %v", err)
	}
}

func TestAnUnprefixedBlobThatLooksPrefixedStillOpens(t *testing.T) {
	// The nonce of today's format is random, so one in four billion starts
	// with the magic. Such a blob must not be lost.
	s := &Sessions{Secret: sessionSecretA, DataKey: dataKeyOne}
	id, secret := uuid.New(), []byte("twenty-byte-secret-!")
	aead, err := (dataKey{root: sessionSecretA}).aead()
	if err != nil {
		t.Fatal(err)
	}
	// The worst case: the bytes after the magic are the id of the configured
	// DATA_KEY, so the blob reads as one of its own and fails to open as that.
	configured := (dataKey{root: dataKeyOne, id: dataKeyID(dataKeyOne)}).header()
	for _, prefix := range [][]byte{configured, append([]byte(sealedMagic), 0, 0, 0, 7)} {
		nonce := append(append([]byte{}, prefix...), 1, 2, 3, 4)
		blob := aead.Seal(nonce, nonce, secret, id[:])
		if keyID, _, ok := parseSealed(blob); !ok || keyID == 0 {
			t.Fatal("the test blob must read as one that names its key")
		}
		if got, stale, err := s.openSecret(id, blob); err != nil || !stale || !bytes.Equal(got, secret) {
			t.Fatalf("prefix %x: stale=%v %v", prefix, stale, err)
		}
	}
}

func TestRecoveryCodesAreCheckedWithThePreviousKeys(t *testing.T) {
	id := uuid.New()
	old := (&Sessions{Secret: sessionSecretA}).recoveryHash(id, "abcdefghjkmn")
	contains := func(s *Sessions) bool {
		for _, h := range s.recoveryHashes(id, "abcdefghjkmn") {
			if bytes.Equal(h, old) {
				return true
			}
		}
		return false
	}
	if !contains(&Sessions{Secret: sessionSecretA, DataKey: dataKeyOne}) {
		t.Fatal("a code made before DATA_KEY was set must keep verifying")
	}
	if !contains(&Sessions{Secret: sessionSecretB, PreviousSecret: sessionSecretA, DataKey: dataKeyOne}) {
		t.Fatal("a code made under the previous session secret must keep verifying")
	}
	if contains(&Sessions{Secret: sessionSecretB, DataKey: dataKeyOne}) {
		t.Fatal("a retired key must not verify anything")
	}
	rotated := &Sessions{Secret: sessionSecretA, DataKey: dataKeyTwo, DataKeyPrevious: dataKeyOne}
	underOne := (&Sessions{Secret: sessionSecretA, DataKey: dataKeyOne}).recoveryHash(id, "abcdefghjkmn")
	if !bytes.Equal(rotated.recoveryHashes(id, "abcdefghjkmn")[1], underOne) || bytes.Equal(rotated.recoveryHash(id, "abcdefghjkmn"), underOne) {
		t.Fatal("new codes go under the new data key, old ones stay checkable")
	}
}

func TestSessionTokensVerifyWithThePreviousSecretOnly(t *testing.T) {
	id := uuid.New()
	old, _ := IssueToken(id, 2, sessionSecretA, time.Hour)
	expired, _ := IssueToken(id, 2, sessionSecretA, -time.Minute)
	foreign, _ := IssueToken(id, 2, strings.Repeat("c", 40), time.Hour)
	s := &Sessions{Secret: sessionSecretB, PreviousSecret: sessionSecretA}

	if c, err := s.parseToken(old); err != nil || c.UserID != id || c.TokenVersion != 2 {
		t.Fatalf("a session signed before the rotation: %v", err)
	}
	if _, err := s.parseToken(expired); err == nil {
		t.Fatal("an expired session under the previous secret")
	}
	if _, err := s.parseToken(foreign); err == nil {
		t.Fatal("a session signed with a key that is neither current nor previous")
	}
	if _, err := (&Sessions{Secret: sessionSecretB}).parseToken(old); err == nil {
		t.Fatal("without the previous secret the old session must not verify")
	}
	if unkeyed, err := IssueToken(id, 2, "", time.Hour); err == nil {
		if _, err := (&Sessions{Secret: sessionSecretB}).parseToken(unkeyed); err == nil {
			t.Fatal("an unset previous secret must not verify a token signed with an empty key")
		}
	}
	fresh, _ := IssueToken(id, 2, s.Secret, time.Hour)
	if _, err := ParseToken(fresh, sessionSecretA); err == nil {
		t.Fatal("nothing is signed with the previous secret")
	}
}

func TestKnownDeviceCookiesSurviveASessionSecretRotation(t *testing.T) {
	uid := uuid.New()
	before := &Sessions{Secret: sessionSecretA}
	rec := httptest.NewRecorder()
	before.MarkDevice(rec, "alice", uid, 4)
	request := func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
		for _, c := range rec.Result().Cookies() {
			r.AddCookie(c)
		}
		return r
	}

	after := &Sessions{Secret: sessionSecretB, PreviousSecret: sessionSecretA}
	if got, gen, ok := after.DeviceClaims(request(), "alice"); !ok || got != uid || gen != 4 {
		t.Fatalf("a device known before the rotation: %v %d %v", got, gen, ok)
	}
	if _, _, ok := (&Sessions{Secret: sessionSecretB}).DeviceClaims(request(), "alice"); ok {
		t.Fatal("without the previous secret the old cookie must not count")
	}
	if _, _, ok := (&Sessions{Secret: sessionSecretB, PreviousSecret: strings.Repeat("c", 40)}).DeviceClaims(request(), "alice"); ok {
		t.Fatal("a cookie under an unrelated secret")
	}
	// The next sign-in sets the cookie under the current secret.
	again := httptest.NewRecorder()
	after.MarkDevice(again, "alice", uid, 4)
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	r.AddCookie(again.Result().Cookies()[0])
	if _, _, ok := (&Sessions{Secret: sessionSecretB}).DeviceClaims(r, "alice"); !ok {
		t.Fatal("the renewed cookie must not need the previous secret")
	}
}

func FuzzSealedBlob(f *testing.F) {
	id := uuid.MustParse("7f8f2c1e-55e7-4a53-9a3e-0d8f4d0f3b11")
	secret := []byte("twenty-byte-secret-!")
	s := &Sessions{Secret: sessionSecretB, PreviousSecret: sessionSecretA, DataKey: dataKeyTwo, DataKeyPrevious: dataKeyOne}
	for _, sealer := range []*Sessions{s, {Secret: sessionSecretA}, {Secret: sessionSecretA, DataKey: dataKeyOne}} {
		blob, err := sealer.sealSecret(id, secret)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(blob)
		f.Add(blob[:len(blob)/2])
	}
	f.Add([]byte{})
	f.Add([]byte(sealedMagic))
	f.Add(append([]byte(sealedMagic), 0, 0, 0, 0))
	f.Add(append([]byte(sealedMagic), 0xff, 0xff, 0xff, 0xff, 1, 2, 3))

	f.Fuzz(func(t *testing.T, blob []byte) {
		keyID, body, ok := parseSealed(blob)
		if ok {
			if len(body) != len(blob)-sealedHeaderLen || !bytes.HasSuffix(blob, body) {
				t.Fatalf("the body is not the rest of the blob: %d of %d", len(body), len(blob))
			}
			if !bytes.Equal(dataKey{id: keyID}.header(), blob[:sealedHeaderLen]) && keyID != 0 {
				t.Fatalf("key id %d does not write the header it was read from", keyID)
			}
		} else if keyID != 0 || body != nil {
			t.Fatal("a blob without a header has no key id")
		}
		// Only a blob one of the keys really sealed opens, and then to the secret.
		if got, _, err := s.openSecret(id, blob); err == nil && !bytes.Equal(got, secret) {
			t.Fatalf("an invented blob opened to %q", got)
		}
	})
}
