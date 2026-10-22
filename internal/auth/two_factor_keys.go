package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// Keys for the second factor are derived from the session secret, one per
// purpose, so the secret itself never keys two different things. Rotating
// JWT_SECRET therefore makes stored authenticator secrets unreadable: the
// affected accounts have to enrol again (an Owner action, see doc/operations.md).
func (s *Sessions) factorKey(purpose string) []byte {
	m := hmac.New(sha256.New, []byte(s.Secret))
	m.Write([]byte("mnema-two-factor-" + purpose + "-v1"))
	return m.Sum(nil)
}

var errUnreadableSecret = errors.New("stored authenticator secret cannot be decrypted")

// sealSecret encrypts a TOTP secret with AES-256-GCM; the account id is the
// associated data, so a secret copied between rows does not decrypt.
func (s *Sessions) sealSecret(userID uuid.UUID, secret []byte) ([]byte, error) {
	aead, err := s.secretAEAD()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("random nonce: %w", err)
	}
	return aead.Seal(nonce, nonce, secret, userID[:]), nil
}

func (s *Sessions) openSecret(userID uuid.UUID, sealed []byte) ([]byte, error) {
	aead, err := s.secretAEAD()
	if err != nil {
		return nil, err
	}
	if len(sealed) < aead.NonceSize() {
		return nil, errUnreadableSecret
	}
	nonce, body := sealed[:aead.NonceSize()], sealed[aead.NonceSize():]
	plain, err := aead.Open(nil, nonce, body, userID[:])
	if err != nil {
		return nil, errUnreadableSecret
	}
	return plain, nil
}

func (s *Sessions) secretAEAD() (cipher.AEAD, error) {
	block, err := aes.NewCipher(s.factorKey("totp-secret"))
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// recoveryHash is the keyed hash recovery codes are stored under. Without the
// server's secret a stolen table cannot be searched for the 60-bit codes.
func (s *Sessions) recoveryHash(userID uuid.UUID, code string) []byte {
	m := hmac.New(sha256.New, s.factorKey("recovery-code"))
	m.Write(userID[:])
	m.Write([]byte(code))
	return m.Sum(nil)
}
