package auth

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// A dataKey is one root secret the second factor's data at rest can be under:
// the sealed authenticator secrets and the recovery code hashes. The keys in
// use are derived from the root, one per purpose, so a root never keys two
// different things.
//
// Key id 0 is the root an instance has without DATA_KEY: the session secret.
// Its blobs carry no header, which is the format every release before key ids
// wrote, so such an instance stores exactly what it always did. A DATA_KEY
// root has a non-zero id that is written in front of every blob sealed under it.
type dataKey struct {
	root string
	id   uint32
}

// sealedMagic starts a blob that names its key: magic, key id (4 bytes, big
// endian), nonce, ciphertext.
const (
	sealedMagic     = "mtk1"
	sealedHeaderLen = len(sealedMagic) + 4
)

// dataKeyID is the id of a DATA_KEY root: four bytes of a keyed hash of it,
// never 0. It only selects the key to try and is not secret.
func dataKeyID(root string) uint32 {
	m := hmac.New(sha256.New, []byte(root))
	m.Write([]byte("mnema-data-key-id-v1"))
	if id := binary.BigEndian.Uint32(m.Sum(nil)); id != 0 {
		return id
	}
	return 1
}

// sealingKey is the key new data is written under: DATA_KEY when it is set,
// otherwise the session secret, as before DATA_KEY existed.
func (s *Sessions) sealingKey() dataKey {
	if s.DataKey != "" {
		return dataKey{root: s.DataKey, id: dataKeyID(s.DataKey)}
	}
	return dataKey{root: s.Secret}
}

// readKeys are the keys stored data may still be under, the sealing key
// first: the previous data key and the session secrets stay readable so a
// rotation, and the move from the session secret to a DATA_KEY, lose nothing.
// DATA_KEY_PREVIOUS without DATA_KEY is the way back: what a data key sealed
// stays readable while new data goes under the session secret again.
func (s *Sessions) readKeys() []dataKey {
	keys := []dataKey{s.sealingKey()}
	add := func(k dataKey) {
		for _, have := range keys {
			if have == k {
				return
			}
		}
		keys = append(keys, k)
	}
	if s.DataKeyPrevious != "" {
		add(dataKey{root: s.DataKeyPrevious, id: dataKeyID(s.DataKeyPrevious)})
	}
	add(dataKey{root: s.Secret})
	if s.PreviousSecret != "" {
		add(dataKey{root: s.PreviousSecret})
	}
	return keys
}

func (k dataKey) derive(purpose string) []byte {
	m := hmac.New(sha256.New, []byte(k.root))
	m.Write([]byte("mnema-two-factor-" + purpose + "-v1"))
	return m.Sum(nil)
}

func (k dataKey) aead() (cipher.AEAD, error) {
	block, err := aes.NewCipher(k.derive("totp-secret"))
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// header is what a blob under this key starts with; empty for key id 0.
func (k dataKey) header() []byte {
	if k.id == 0 {
		return nil
	}
	return binary.BigEndian.AppendUint32([]byte(sealedMagic), k.id)
}

// seal encrypts with AES-256-GCM. The account id is associated data, so a
// secret copied between rows does not decrypt; so is the header.
func (k dataKey) seal(userID uuid.UUID, plain []byte) ([]byte, error) {
	aead, err := k.aead()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("random nonce: %w", err)
	}
	header := k.header()
	out := append(append([]byte{}, header...), nonce...)
	return aead.Seal(out, nonce, plain, append(header, userID[:]...)), nil
}

// open decrypts what follows the header: nonce and ciphertext.
func (k dataKey) open(userID uuid.UUID, body []byte) ([]byte, bool) {
	aead, err := k.aead()
	if err != nil || len(body) < aead.NonceSize() {
		return nil, false
	}
	plain, err := aead.Open(nil, body[:aead.NonceSize()], body[aead.NonceSize():], append(k.header(), userID[:]...))
	return plain, err == nil
}

// parseSealed splits a blob that names its key into the key id and the rest.
// ok is false for a blob without the header, which is today's format (key id
// 0) or damage.
func parseSealed(blob []byte) (id uint32, body []byte, ok bool) {
	if len(blob) < sealedHeaderLen || !bytes.HasPrefix(blob, []byte(sealedMagic)) {
		return 0, nil, false
	}
	return binary.BigEndian.Uint32(blob[len(sealedMagic):sealedHeaderLen]), blob[sealedHeaderLen:], true
}

var errUnreadableSecret = errors.New("stored authenticator secret cannot be decrypted")

// sealSecret encrypts a TOTP secret under the sealing key.
func (s *Sessions) sealSecret(userID uuid.UUID, secret []byte) ([]byte, error) {
	return s.sealingKey().seal(userID, secret)
}

// openSecret decrypts a stored TOTP secret with whichever configured key it
// is under. stale reports that this was not the sealing key: the caller seals
// it again.
func (s *Sessions) openSecret(userID uuid.UUID, sealed []byte) (secret []byte, stale bool, err error) {
	keys := s.readKeys()
	if id, body, ok := parseSealed(sealed); ok && id != 0 {
		for _, k := range keys {
			if k.id != id {
				continue
			}
			if plain, ok := k.open(userID, body); ok {
				return plain, k != keys[0], nil
			}
		}
	}
	// No header, or random bytes that only look like one: today's format.
	for _, k := range keys {
		if k.id != 0 {
			continue
		}
		if plain, ok := k.open(userID, sealed); ok {
			return plain, k != keys[0], nil
		}
	}
	return nil, false, errUnreadableSecret
}

// recoveryHash is the keyed hash a new recovery code is stored under. Without
// the server's key a stolen table cannot be searched for the 60-bit codes.
func (s *Sessions) recoveryHash(userID uuid.UUID, code string) []byte {
	return s.sealingKey().recoveryHash(userID, code)
}

// recoveryHashes is the hash of code under every readable key. A stored hash
// cannot be moved to a new key (the code is not known), so codes made before
// a rotation keep verifying until the previous key is removed.
func (s *Sessions) recoveryHashes(userID uuid.UUID, code string) [][]byte {
	keys := s.readKeys()
	out := make([][]byte, 0, len(keys))
	for _, k := range keys {
		out = append(out, k.recoveryHash(userID, code))
	}
	return out
}

func (k dataKey) recoveryHash(userID uuid.UUID, code string) []byte {
	m := hmac.New(sha256.New, k.derive("recovery-code"))
	m.Write(userID[:])
	m.Write([]byte(code))
	return m.Sum(nil)
}
