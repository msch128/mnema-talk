package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
)

var (
	ErrNativeUnauthorized  = errors.New("native session unauthorized")
	ErrNativeLimit         = errors.New("native session limit reached")
	ErrNativeRefreshWait   = errors.New("native session refresh rate limited")
	errNativeSerialization = errors.New("native secrets require explicit wire conversion")
	errNativeEntropy       = errors.New("native session entropy unavailable")
)

// nativeSecret is deliberately private and never passed to a generic JSON DTO.
// An inaccessible enclosing field prevents fmt from calling nested Format
// methods. Pointer storage also prevents its reflective fallback from walking
// the token bytes. The extra pointer indirection matters: fmt's invalid-verb
// fallback resets depth and dereferences a pointer directly to an array.
// This protects accidental formatting, not hostile code in the same process or
// deliberate memory inspection.
type nativeSecret struct{ value **[32]byte }

func newNativeSecret() nativeSecret {
	bytes := new([32]byte)
	return nativeSecret{value: &bytes}
}

func (nativeSecret) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, "[native secret redacted]")
}

func (nativeSecret) String() string               { return "[native secret redacted]" }
func (nativeSecret) GoString() string             { return "[native secret redacted]" }
func (nativeSecret) LogValue() slog.Value         { return slog.StringValue("[native secret redacted]") }
func (nativeSecret) MarshalJSON() ([]byte, error) { return nil, errNativeSerialization }
func (v nativeSecret) wire() string {
	if v.value == nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString((*v.value)[:])
}
func (v nativeSecret) digest() [32]byte {
	if v.value == nil {
		return [32]byte{}
	}
	return sha256.Sum256((*v.value)[:])
}

func parseNativeSecret(raw string) (nativeSecret, error) {
	if len(raw) != 43 {
		return nativeSecret{}, ErrNativeUnauthorized
	}
	for i := range raw {
		b := raw[i]
		if !(b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '-' || b == '_') {
			return nativeSecret{}, ErrNativeUnauthorized
		}
	}
	// Strict decoding still ignores CR/LF, hence the explicit alphabet above.
	secret := newNativeSecret()
	n, err := base64.RawURLEncoding.Strict().Decode((*secret.value)[:], []byte(raw))
	if err != nil || n != len(*secret.value) || secret.wire() != raw {
		clear((*secret.value)[:])
		return nativeSecret{}, ErrNativeUnauthorized
	}
	return secret, nil
}

func generateNativeSecrets(random io.Reader) (nativeSecret, nativeSecret, error) {
	access := newNativeSecret()
	refresh := newNativeSecret()
	if _, err := io.ReadFull(random, (*access.value)[:]); err != nil {
		clear((*access.value)[:])
		return nativeSecret{}, nativeSecret{}, errNativeEntropy
	}
	if _, err := io.ReadFull(random, (*refresh.value)[:]); err != nil || **access.value == **refresh.value {
		clear((*access.value)[:])
		clear((*refresh.value)[:])
		return nativeSecret{}, nativeSecret{}, errNativeEntropy
	}
	return access, refresh, nil
}

// Error text is constant even when the cause contains SQL details or secrets.
// Unwrap preserves cancellation/SQLSTATE inspection without logging the cause.
type nativeStoreError struct{ cause *error }

func (nativeStoreError) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, "native session storage failed")
}

func (nativeStoreError) Error() string    { return "native session storage failed" }
func (nativeStoreError) GoString() string { return "native session storage failed" }
func (nativeStoreError) LogValue() slog.Value {
	return slog.StringValue("native session storage failed")
}
func (nativeStoreError) MarshalJSON() ([]byte, error) { return nil, errNativeSerialization }
func (e nativeStoreError) Unwrap() error {
	if e.cause == nil {
		return nil
	}
	return *e.cause
}

func nativeStoreFailure(err error) error {
	return nativeStoreError{cause: &err}
}
