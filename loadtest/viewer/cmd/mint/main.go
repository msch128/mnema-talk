// Command mint signs session tokens for the load-test users: HS256 JWTs with
// the claims of internal/auth IssueToken. It reads "id,token_version" lines
// (psql -At -F, output) on stdin and writes [{"id": "...", "token": "..."}] as
// JSON to the file named by OUT (mode 0600) or to stdout.
//
//	JWT_SECRET=... OUT=tokens.json mint < ids.csv
//
// TTL_S is the lifetime in seconds (default 8 h). The secret is read from the
// environment only and nothing it signs is ever printed except into OUT.
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

type claims struct {
	UID string `json:"uid"`
	TV  int    `json:"tv"`
	Iss string `json:"iss"`
	Sub string `json:"sub"`
	Iat int64  `json:"iat"`
	Nbf int64  `json:"nbf"`
	Exp int64  `json:"exp"`
}

type minted struct {
	ID    string `json:"id"`
	Token string `json:"token"`
}

func b64(v any) string {
	b, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(b)
}

// sign returns the HS256 token for a user.
func sign(secret, id string, tokenVersion int, now time.Time, ttl time.Duration) string {
	head := b64(map[string]string{"alg": "HS256", "typ": "JWT"}) + "." + b64(claims{
		UID: id, TV: tokenVersion, Iss: "mnema-talk", Sub: id,
		Iat: now.Unix(), Nbf: now.Unix(), Exp: now.Add(ttl).Unix(),
	})
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(head))
	return head + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// parseLine reads "id,token_version" (a comma or a pipe separates them).
func parseLine(line string) (string, int, error) {
	id, tv, ok := strings.Cut(strings.ReplaceAll(line, "|", ","), ",")
	if !ok || id == "" {
		return "", 0, errors.New("expected id,token_version")
	}
	n, err := strconv.Atoi(strings.TrimSpace(tv))
	if err != nil {
		return "", 0, errors.New("token_version is not a number")
	}
	return strings.TrimSpace(id), n, nil
}

// mint signs a token for every non-empty line of in.
func mint(in io.Reader, secret string, now time.Time, ttl time.Duration) ([]minted, error) {
	raw, err := io.ReadAll(in)
	if err != nil {
		return nil, err
	}
	out := []minted{}
	for i, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		id, tv, err := parseLine(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		out = append(out, minted{ID: id, Token: sign(secret, id, tv, now, ttl)})
	}
	return out, nil
}

func run() error {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return errors.New("JWT_SECRET is required")
	}
	ttl := 8 * time.Hour
	if v, err := strconv.Atoi(os.Getenv("TTL_S")); err == nil && v > 0 {
		ttl = time.Duration(v) * time.Second
	}
	tokens, err := mint(os.Stdin, secret, time.Now(), ttl)
	if err != nil {
		return err
	}
	b, err := json.Marshal(tokens)
	if err != nil {
		return err
	}
	if out := os.Getenv("OUT"); out != "" {
		return os.WriteFile(out, b, 0o600)
	}
	_, err = os.Stdout.Write(b)
	return err
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "mint:", err)
		os.Exit(2)
	}
}
