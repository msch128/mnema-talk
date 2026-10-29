package turnrelay

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// authority mints and checks TURN credentials in the TURN REST style
// (draft-uberti-behave-turn-rest): the username is "<expiry unix>:<member
// id>" and the password the HMAC of the username under a secret only this
// process knows. The secret is random per start, so credentials never
// outlive a restart (browsers fetch a new set on every join) and nothing is
// stored or shared; it is deliberately not WEBRTC_TURN_SECRET, which belongs
// to an external coturn.
type authority struct{ secret []byte }

func newAuthority() (*authority, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	return &authority{secret: secret}, nil
}

func (a *authority) password(username string) string {
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(username))
	return base64.RawStdEncoding.EncodeToString(mac.Sum(nil))
}

// mint returns the credentials of one member, valid until now+ttl.
func (a *authority) mint(user uuid.UUID, now time.Time, ttl time.Duration) (username, password string, expires time.Time) {
	expires = now.Add(ttl).Truncate(time.Second)
	username = strconv.FormatInt(expires.Unix(), 10) + ":" + user.String()
	return username, a.password(username), expires
}

var (
	errMalformed = errors.New("malformed username")
	errExpired   = errors.New("credential expired")
)

// parseUsername splits "<expiry>:<member id>".
func parseUsername(username string) (user uuid.UUID, expires time.Time, err error) {
	exp, id, ok := strings.Cut(username, ":")
	if !ok {
		return uuid.Nil, time.Time{}, errMalformed
	}
	secs, err := strconv.ParseInt(exp, 10, 64)
	if err != nil || secs <= 0 {
		return uuid.Nil, time.Time{}, errMalformed
	}
	user, err = uuid.Parse(id)
	if err != nil {
		return uuid.Nil, time.Time{}, errMalformed
	}
	return user, time.Unix(secs, 0), nil
}

// check validates a username: well formed and not expired. The password is not
// compared here: the TURN exchange proves knowledge of it through the message
// integrity of the request, which is verified against the key derived from it.
func (a *authority) check(username string, now time.Time) (uuid.UUID, time.Time, error) {
	user, expires, err := parseUsername(username)
	if err != nil {
		return uuid.Nil, time.Time{}, err
	}
	if !expires.After(now) {
		return uuid.Nil, time.Time{}, errExpired
	}
	return user, expires, nil
}
