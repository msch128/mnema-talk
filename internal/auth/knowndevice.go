package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Known devices decide who may keep guessing once an account's guess cap has
// tripped. A device is known for an account when it carries a signed cookie
// set by an earlier successful login. The cookie is an HMAC under a key
// derived from JWT_SECRET, so it survives restarts and address changes and
// nothing about it is stored on the server. A cookie under the previous
// secret (JWT_SECRET_PREVIOUS) still counts; the next sign-in sets one under
// the current secret. Unknown devices get one password
// check per minute per client address while the cap is on (see verifyLogin),
// so a botnet learns next to nothing and the owner on a new phone still gets in.
const (
	knownDeviceTTL     = 365 * 24 * time.Hour
	knownDeviceContext = "mnema-known-device|"
)

func deviceCookieName(key []byte, account string) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte("name|" + account))
	return "mnema_dev_" + hex.EncodeToString(m.Sum(nil)[:6])
}

// deviceKey derives the cookie key from a session secret, so the secret
// itself is never used directly as an HMAC key for a second purpose.
func deviceKey(secret string) []byte {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte("mnema-known-device-key-v1"))
	return m.Sum(nil)
}

func deviceMAC(key []byte, account string, uid uuid.UUID, gen int, exp int64) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(knownDeviceContext + account + "|" + uid.String() + "|" + strconv.Itoa(gen) + "|" + strconv.FormatInt(exp, 10)))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// DeviceMAC signs a known-device cookie value for account (the tests forge
// cookies with it).
func (s *Sessions) DeviceMAC(account string, uid uuid.UUID, gen int, exp int64) string {
	return deviceMAC(deviceKey(s.Secret), account, uid, gen, exp)
}

// DeviceClaims returns the user id and generation (the user's token_version at
// issue time) of a valid, unexpired known-device cookie for account. Whether
// they are still current is checked against the database by isKnownDevice.
func (s *Sessions) DeviceClaims(r *http.Request, account string) (uuid.UUID, int, bool) {
	if uid, gen, ok := deviceClaims(r, deviceKey(s.Secret), account); ok {
		return uid, gen, true
	}
	if s.PreviousSecret == "" {
		return uuid.Nil, 0, false
	}
	return deviceClaims(r, deviceKey(s.PreviousSecret), account)
}

// deviceClaims reads the cookie signed with key; its name depends on the key too.
func deviceClaims(r *http.Request, key []byte, account string) (uuid.UUID, int, bool) {
	c, err := r.Cookie(deviceCookieName(key, account))
	if err != nil {
		return uuid.Nil, 0, false
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 4 {
		return uuid.Nil, 0, false
	}
	uid, err := uuid.Parse(parts[0])
	if err != nil {
		return uuid.Nil, 0, false
	}
	gen, err := strconv.Atoi(parts[1])
	if err != nil {
		return uuid.Nil, 0, false
	}
	exp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return uuid.Nil, 0, false
	}
	if !hmac.Equal([]byte(parts[3]), []byte(deviceMAC(key, account, uid, gen, exp))) {
		return uuid.Nil, 0, false
	}
	return uid, gen, true
}

// markDevice sets the known-device cookie for account, bound to the user id
// and the user's current token_version: a password change or reset, "log out
// everywhere", an administrator revoking sessions and disabling the account
// all bump it, which retires every device cookie issued before.
func (s *Sessions) MarkDevice(w http.ResponseWriter, account string, uid uuid.UUID, gen int) {
	exp := time.Now().Add(knownDeviceTTL).Unix()
	http.SetCookie(w, &http.Cookie{
		Name: deviceCookieName(deviceKey(s.Secret), account),
		Value: uid.String() + "." + strconv.Itoa(gen) + "." + strconv.FormatInt(exp, 10) +
			"." + s.DeviceMAC(account, uid, gen, exp),
		Path:     "/api/auth",
		HttpOnly: true,
		Secure:   s.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(knownDeviceTTL.Seconds()),
	})
}
