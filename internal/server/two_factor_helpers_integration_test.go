//go:build integration

package server

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// authenticator is an independent TOTP implementation (RFC 6238) for tests.
type authenticator struct{ secret []byte }

func (a authenticator) code(offset int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(time.Now().Unix()/30+offset))
	m := hmac.New(sha1.New, a.secret)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	n := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", n%1_000_000)
}

type enrolled struct {
	auth     authenticator
	recovery []string
}

// enroll turns two-factor sign-in on for the client's account.
func (a *app) enroll(c *client, password string) enrolled {
	a.t.Helper()
	res := c.post("/api/users/me/2fa/setup", map[string]string{"current_password": password})
	if res.status != http.StatusOK {
		a.t.Fatalf("setup: %d %s", res.status, res.body)
	}
	var setup struct {
		Secret     string `json:"secret"`
		OtpauthURI string `json:"otpauth_uri"`
	}
	res.decode(a.t, &setup)
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(setup.Secret)
	if err != nil {
		a.t.Fatal(err)
	}
	au := authenticator{secret: secret}
	res = c.post("/api/users/me/2fa/enable", map[string]string{"code": au.code(0)})
	if res.status != http.StatusOK {
		a.t.Fatalf("enable: %d %s", res.status, res.body)
	}
	var codes struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	res.decode(a.t, &codes)
	return enrolled{auth: au, recovery: codes.RecoveryCodes}
}

// freshStep forgets the newest accepted step, so a test can sign in twice
// within the same 30 seconds.
func (a *app) freshStep() {
	a.t.Helper()
	if _, err := a.db.Exec(context.Background(), `UPDATE user_totp SET last_step = 0`); err != nil {
		a.t.Fatal(err)
	}
}

type challenge struct {
	TwoFactorRequired bool   `json:"two_factor_required"`
	Challenge         string `json:"challenge"`
	ExpiresIn         int    `json:"expires_in"`
}

// password signs in with the password only and returns the answer.
func (a *app) password(c *client, username, password string) response {
	return c.post("/api/auth/login", map[string]string{"username": username, "password": password})
}

// startLogin runs step one for a 2FA account and returns the challenge.
func (a *app) startLogin(c *client, username, password string) challenge {
	a.t.Helper()
	res := a.password(c, username, password)
	if res.status != http.StatusAccepted {
		a.t.Fatalf("step one: %d %s", res.status, res.body)
	}
	var ch challenge
	res.decode(a.t, &ch)
	return ch
}

func secondStep(c *client, ch, code string) response {
	return c.post("/api/auth/login/2fa", map[string]string{"challenge": ch, "code": code})
}

// plantChallenge inserts a fresh challenge directly, so tests that need many
// of them do not spend the sign-in rate limit.
func (a *app) plantChallenge(userID uuid.UUID) string {
	a.t.Helper()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		a.t.Fatal(err)
	}
	plain := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(plain))
	_, err := a.db.Exec(context.Background(), `INSERT INTO login_challenges (token_hash, user_id, token_version, expires_at)
		VALUES ($1, $2, (SELECT token_version FROM users WHERE id = $2), NOW() + INTERVAL '5 minutes')`, sum[:], userID)
	if err != nil {
		a.t.Fatal(err)
	}
	return plain
}

func (a *app) countRows(query string, args ...any) int {
	a.t.Helper()
	var n int
	if err := a.db.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		a.t.Fatal(err)
	}
	return n
}

func wrongCode(offset int, au authenticator) string {
	// A six-digit string that is none of the codes the window accepts.
	for i := 0; i < 1000; i++ {
		c := fmt.Sprintf("%06d", (offset*7919+i*104729)%1_000_000)
		if c != au.code(-1) && c != au.code(0) && c != au.code(1) {
			return c
		}
	}
	return "000000"
}
