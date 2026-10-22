// Package push delivers Web Push notifications (RFC 8030) for new messages.
//
// Payloads are encrypted for the receiving browser (RFC 8291) and the request
// is signed with the server's VAPID key (RFC 8292), so a push service sees
// neither the sender, the channel nor the text. The encryption and signing are
// done by github.com/SherClockHolmes/webpush-go (MIT, pinned in go.mod); this
// package owns everything around it: who is notified, the key pair, the
// subscriptions, and refusing to be used as a request proxy.
package push

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
)

// Keys is the server's VAPID key pair in base64url (unpadded). The private key
// never leaves the server: it is not logged and no endpoint returns it.
type Keys struct {
	Public  string
	Private string
}

const (
	publicKeyLen  = 65 // uncompressed P-256 point
	privateKeyLen = 32
)

// valid reports whether both halves decode to the sizes a P-256 key has.
func (k Keys) valid() bool {
	pub, errPub := base64.RawURLEncoding.DecodeString(k.Public)
	priv, errPriv := base64.RawURLEncoding.DecodeString(k.Private)
	return errPub == nil && errPriv == nil && len(pub) == publicKeyLen && len(priv) == privateKeyLen
}

// LoadKeys returns the stored key pair and creates it on first use. Several
// servers starting at once agree on whichever row was written first. A stored
// pair that does not decode is an error, never silently replaced: replacing it
// would end every subscription without anyone noticing.
func LoadKeys(ctx context.Context, q db.Querier) (Keys, error) {
	keys, err := readKeys(ctx, q)
	if errors.Is(err, pgx.ErrNoRows) {
		if keys, err = createKeys(ctx, q); err != nil {
			return Keys{}, err
		}
	} else if err != nil {
		return Keys{}, fmt.Errorf("read vapid keys: %w", err)
	}
	if !keys.valid() {
		return Keys{}, errors.New("stored vapid keys are malformed")
	}
	return keys, nil
}

func readKeys(ctx context.Context, q db.Querier) (Keys, error) {
	var k Keys
	err := q.QueryRow(ctx, `SELECT public_key, private_key FROM push_vapid`).Scan(&k.Public, &k.Private)
	return k, err
}

func createKeys(ctx context.Context, q db.Querier) (Keys, error) {
	private, public, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return Keys{}, fmt.Errorf("generate vapid keys: %w", err)
	}
	if _, err := q.Exec(ctx, `
		INSERT INTO push_vapid (public_key, private_key) VALUES ($1, $2)
		ON CONFLICT (singleton) DO NOTHING`, public, private); err != nil {
		return Keys{}, fmt.Errorf("store vapid keys: %w", err)
	}
	keys, err := readKeys(ctx, q)
	if err != nil {
		return Keys{}, fmt.Errorf("read vapid keys: %w", err)
	}
	return keys, nil
}

// fallbackSubject is used when PUBLIC_URL is not https (development): push
// services want a contact, and the real services accept any mailto address.
const fallbackSubject = "mailto:push@mnema.invalid"

// Subject picks the VAPID contact: the configured one, else PUBLIC_URL when it
// is https, else a placeholder address.
func Subject(configured, publicURL string) string {
	switch {
	case configured != "":
		return configured
	case strings.HasPrefix(publicURL, "https://"):
		return publicURL
	}
	return fallbackSubject
}

// subscriber is the form the library wants: an https URL as is, an e-mail
// address without its mailto: scheme (the library adds it).
func subscriber(subject string) string {
	return strings.TrimPrefix(subject, "mailto:")
}
