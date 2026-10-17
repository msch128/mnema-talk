package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/msch128/mnema-talk/internal/httpx"
)

// Known devices decide who may keep guessing once an account's guess cap has
// tripped. A device is known for an account when it carries a signed cookie
// set by an earlier successful login (browsers), or when its address (IPv6
// /64) signed in to that account recently (native clients, no cookie jar).
// Everyone else is turned away with 429 without a password check, so a botnet
// cannot keep testing guesses against the Owner while the cap is on.
const (
	knownDeviceTTL     = 365 * 24 * time.Hour
	knownIPTTL         = 30 * 24 * time.Hour
	knownIPMax         = 10000
	knownDeviceContext = "mnema-known-device|"
)

func (s *Sessions) deviceCookieName(account string) string {
	sum := sha256.Sum256([]byte(account))
	return "mnema_dev_" + hex.EncodeToString(sum[:6])
}

func (s *Sessions) deviceMAC(account string, exp int64) string {
	m := hmac.New(sha256.New, []byte(s.Secret))
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

// knownAddrs remembers which client addresses signed in to which account.
type knownAddrs struct {
	mu sync.Mutex
	m  map[string]time.Time
}

func (k *knownAddrs) has(account, ip string) bool {
	if k == nil {
		return false
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	t, ok := k.m[account+"|"+ip]
	return ok && time.Since(t) < knownIPTTL
}

func (k *knownAddrs) add(account, ip string) {
	if k == nil {
		return
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.m == nil {
		k.m = map[string]time.Time{}
	}
	if len(k.m) >= knownIPMax {
		for key, t := range k.m {
			if time.Since(t) >= knownIPTTL {
				delete(k.m, key)
			}
		}
		if len(k.m) >= knownIPMax { // still full: forget an arbitrary entry
			for key := range k.m {
				delete(k.m, key)
				break
			}
		}
	}
	k.m[account+"|"+ip] = time.Now()
}

// isKnownDevice reports whether r comes from a device that signed in to account before.
func (h *Handler) isKnownDevice(r *http.Request, account string) bool {
	return h.Sessions.hasDeviceCookie(r, account) || h.knownIPs.has(account, httpx.ClientIPKey(r))
}
