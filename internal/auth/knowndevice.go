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
)

// Known devices decide who may keep guessing once an account's guess cap has
// tripped. A device is known for an account when it carries a signed cookie
// set by an earlier successful login. The cookie is an HMAC under a key
// derived from JWT_SECRET, so it survives restarts and address changes and
// nothing about it is stored on the server. Unknown devices get one password
// check per minute per client address while the cap is on (see verifyLogin),
// so a botnet learns next to nothing and the owner on a new phone still gets in.
const (
	knownDeviceTTL     = 365 * 24 * time.Hour
	knownDeviceContext = "mnema-known-device|"
)

func (s *Sessions) deviceCookieName(account string) string {
	m := hmac.New(sha256.New, s.deviceKey())
	m.Write([]byte("name|" + account))
	return "mnema_dev_" + hex.EncodeToString(m.Sum(nil)[:6])
}

// deviceKey derives the cookie key from the session secret, so the secret
// itself is never used directly as an HMAC key for a second purpose.
func (s *Sessions) deviceKey() []byte {
	m := hmac.New(sha256.New, []byte(s.Secret))
	m.Write([]byte("mnema-known-device-key-v1"))
	return m.Sum(nil)
}

func (s *Sessions) deviceMAC(account string, exp int64) string {
	m := hmac.New(sha256.New, s.deviceKey())
	m.Write([]byte(knownDeviceContext + account + "|" + strconv.FormatInt(exp, 10)))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// hasDeviceCookie reports a valid, unexpired known-device cookie for account.
func (s *Sessions) hasDeviceCookie(r *http.Request, account string) bool {
	c, err := r.Cookie(s.deviceCookieName(account))
	if err != nil {
		return false
	}
	expStr, mac, ok := strings.Cut(c.Value, ".")
	if !ok {
		return false
	}
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	return hmac.Equal([]byte(mac), []byte(s.deviceMAC(account, exp)))
}

// markDevice sets the known-device cookie for account.
func (s *Sessions) markDevice(w http.ResponseWriter, account string) {
	exp := time.Now().Add(knownDeviceTTL).Unix()
	http.SetCookie(w, &http.Cookie{
		Name:     s.deviceCookieName(account),
		Value:    strconv.FormatInt(exp, 10) + "." + s.deviceMAC(account, exp),
		Path:     "/api/auth",
		HttpOnly: true,
		Secure:   s.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(knownDeviceTTL.Seconds()),
	})
}

// isKnownDevice reports whether r carries a valid known-device cookie for account.
func (h *Handler) isKnownDevice(r *http.Request, account string) bool {
	return h.Sessions.hasDeviceCookie(r, account)
}
