package push

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"testing"

	"golang.org/x/crypto/hkdf"
)

// receive is the receiving side of RFC 8291 (the browser's job), written from
// section 3.4 of the RFC: it opens an aes128gcm push body with the user agent's
// private key and authentication secret. The test below first proves it on the
// RFC's own example, then uses it to check what the sender produces.
func receive(t *testing.T, body []byte, ua *ecdh.PrivateKey, authSecret []byte) []byte {
	t.Helper()
	salt := body[:16]
	idLen := int(body[20])
	asPublic := body[21 : 21+idLen]
	ciphertext := body[21+idLen:]

	asKey, err := ecdh.P256().NewPublicKey(asPublic)
	if err != nil {
		t.Fatalf("application server key: %v", err)
	}
	secret, err := ua.ECDH(asKey)
	if err != nil {
		t.Fatal(err)
	}
	info := append(append([]byte("WebPush: info\x00"), ua.PublicKey().Bytes()...), asPublic...)
	ikm := expand(t, hkdf.New(sha256.New, secret, authSecret, info), 32)
	cek := expand(t, hkdf.New(sha256.New, ikm, salt, []byte("Content-Encoding: aes128gcm\x00")), 16)
	nonce := expand(t, hkdf.New(sha256.New, ikm, salt, []byte("Content-Encoding: nonce\x00")), 12)

	block, err := aes.NewCipher(cek)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	// The last record ends with the padding delimiter 0x02 and zero padding.
	end := len(plain)
	for end > 0 && plain[end-1] == 0 {
		end--
	}
	if end == 0 || plain[end-1] != 2 {
		t.Fatalf("missing padding delimiter in %x", plain)
	}
	return plain[:end-1]
}

func expand(t *testing.T, r io.Reader, n int) []byte {
	t.Helper()
	out := make([]byte, n)
	if _, err := io.ReadFull(r, out); err != nil {
		t.Fatal(err)
	}
	return out
}

func b64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// RFC 8291 appendix A: the example message and the values it was made from.
func TestReceiverOpensTheRFC8291Example(t *testing.T) {
	uaPrivate := b64(t, "q1dXpw3UpT5VOmu_cf_v6ih07Aems3njxI-JWgLcM94")
	authSecret := b64(t, "BTBZMqHH6r4Tts7J_aSIgg")
	body := b64(t, "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPTpK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN")
	ua, err := ecdh.P256().NewPrivateKey(uaPrivate)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := base64.RawURLEncoding.EncodeToString(ua.PublicKey().Bytes()), "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"; got != want {
		t.Fatalf("user agent public key %s, RFC has %s", got, want)
	}
	if got := string(receive(t, body, ua, authSecret)); got != "When I grow up, I want to be a watermelon" {
		t.Fatalf("plaintext %q", got)
	}
}

// browser is a stand-in for a real browser's push subscription: its key pair
// and secret, plus what it needs to open a push.
type browser struct {
	key  *ecdh.PrivateKey
	auth []byte
}

func newBrowser(t *testing.T) browser {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	secret := make([]byte, 16)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	return browser{key: key, auth: secret}
}

func (b browser) p256dh() string {
	return base64.RawURLEncoding.EncodeToString(b.key.PublicKey().Bytes())
}
func (b browser) secret() string { return base64.RawURLEncoding.EncodeToString(b.auth) }
func (b browser) open(t *testing.T, body []byte) []byte {
	t.Helper()
	return receive(t, body, b.key, b.auth)
}
