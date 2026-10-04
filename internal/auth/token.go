package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Claims identify a session. Authorization data (role, names) is deliberately not
// trusted from the token: the middleware reloads the user on every request, and
// TokenVersion lets the server revoke all sessions of a user at once.
type Claims struct {
	UserID       uuid.UUID `json:"uid"`
	TokenVersion int       `json:"tv"`
	jwt.RegisteredClaims
}

const issuer = "mnema-talk"

// IssueToken signs a session token valid for ttl.
func IssueToken(userID uuid.UUID, tokenVersion int, secret string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:       userID,
		TokenVersion: tokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

// ParseToken validates signature, algorithm, issuer and expiry.
func ParseToken(tokenString, secret string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		return []byte(secret), nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(issuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, err
	}
	if claims.UserID == uuid.Nil {
		return nil, errors.New("token has no user id")
	}
	return claims, nil
}
