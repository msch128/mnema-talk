package push

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

const (
	// MaxDevices is how many browsers one account may enable push on.
	MaxDevices = 10
	// maxFailures is how many failed deliveries in a row end a subscription
	// (rejections other than "gone", which end it at once).
	maxFailures = 10
)

// Subscription is one browser a member enabled push on.
type Subscription struct {
	ID            uuid.UUID  `json:"id" format:"uuid"`
	Label         string     `json:"label" maxLength:"64"`
	Fingerprint   string     `json:"fingerprint"`
	CreatedAt     time.Time  `json:"created_at" format:"date-time"`
	LastSuccessAt *time.Time `json:"last_success_at" format:"date-time" extensions:"x-nullable"`
}

// target is what delivery needs of a subscription.
type target struct {
	id       uuid.UUID
	userID   uuid.UUID
	endpoint string
	p256dh   string
	auth     string
}

// Fingerprint identifies an endpoint without disclosing it: the SHA-256 of the
// endpoint, hex, first 32 characters. A browser computes the same from its own
// subscription to recognise "this device" in the list. The endpoint is a
// capability of the push service, so the API never returns it.
func Fingerprint(endpoint string) string {
	sum := sha256.Sum256([]byte(endpoint))
	return hex.EncodeToString(sum[:])[:32]
}

// errTooManyDevices is the refusal when an account is at MaxDevices.
var errTooManyDevices = httpx.ErrConflict(fmt.Sprintf("an account can use push on at most %d devices; remove one first", MaxDevices))

// newSubscription is a validated registration. sessionID and sessionExpires
// belong to the session that registers it (see validSession).
type newSubscription struct {
	endpoint, p256dh, auth, label string
	sessionID                     string
	sessionExpires                *time.Time
}

// validSession is the SQL condition that a subscription s (joined to its
// user u) still belongs to a live session: the session was not signed out, has
// not expired, and the user's token_version has not moved on since.
const validSession = `u.token_version = s.token_version
	AND (s.session_expires_at IS NULL OR s.session_expires_at > NOW())
	AND (s.session_id = '' OR NOT EXISTS (SELECT 1 FROM revoked_sessions r WHERE r.jti = s.session_id))`

// pruneEnded deletes the subscriptions whose session ended, and returns how many.
func pruneEnded(ctx context.Context, q db.Querier) (int64, error) {
	tag, err := q.Exec(ctx, `
		DELETE FROM push_subscriptions s USING users u
		WHERE u.id = s.user_id AND NOT (`+validSession+`)`)
	if err != nil {
		return 0, fmt.Errorf("prune push subscriptions: %w", err)
	}
	return tag.RowsAffected(), nil
}

// upsert stores or refreshes the browser's subscription for userID and returns
// it. An endpoint registered before by another account moves to this one.
func upsert(ctx context.Context, p *db.Pool, userID uuid.UUID, in newSubscription) (Subscription, error) {
	var out Subscription
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		// Serialise registrations of one account so the device limit holds.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "push:"+userID.String()); err != nil {
			return err
		}
		var others int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM push_subscriptions WHERE user_id = $1 AND endpoint <> $2`, userID, in.endpoint).Scan(&others); err != nil {
			return err
		}
		if others >= MaxDevices {
			return errTooManyDevices
		}
		return tx.QueryRow(ctx, `
			INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth, label, session_id, session_expires_at, token_version)
			VALUES ($1, $2, $3, $4, $5, $6, $7, (SELECT token_version FROM users WHERE id = $1))
			ON CONFLICT (endpoint) DO UPDATE
			SET user_id = EXCLUDED.user_id, p256dh = EXCLUDED.p256dh, auth = EXCLUDED.auth,
			    label = EXCLUDED.label, failures = 0, session_id = EXCLUDED.session_id,
			    session_expires_at = EXCLUDED.session_expires_at, token_version = EXCLUDED.token_version
			RETURNING id, label, created_at, last_success_at`,
			userID, in.endpoint, in.p256dh, in.auth, in.label, in.sessionID, in.sessionExpires).
			Scan(&out.ID, &out.Label, &out.CreatedAt, &out.LastSuccessAt)
	})
	if err != nil {
		if _, ok := httpx.AsAPIError(err); ok {
			return Subscription{}, err
		}
		return Subscription{}, fmt.Errorf("store push subscription: %w", err)
	}
	out.Fingerprint = Fingerprint(in.endpoint)
	return out, nil
}

// list returns the account's subscriptions, oldest first.
func list(ctx context.Context, q db.Querier, userID uuid.UUID) ([]Subscription, error) {
	rows, err := q.Query(ctx, `
		SELECT s.id, s.label, s.endpoint, s.created_at, s.last_success_at
		FROM push_subscriptions s JOIN users u ON u.id = s.user_id
		WHERE s.user_id = $1 AND `+validSession+` ORDER BY s.created_at, s.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("list push subscriptions: %w", err)
	}
	defer rows.Close()
	out := []Subscription{}
	for rows.Next() {
		var s Subscription
		var endpoint string
		if err := rows.Scan(&s.ID, &s.Label, &endpoint, &s.CreatedAt, &s.LastSuccessAt); err != nil {
			return nil, fmt.Errorf("list push subscriptions: %w", err)
		}
		s.Fingerprint = Fingerprint(endpoint)
		out = append(out, s)
	}
	return out, rows.Err()
}

// remove deletes one of the account's subscriptions and reports whether it existed.
func remove(ctx context.Context, q db.Querier, userID, id uuid.UUID) (bool, error) {
	tag, err := q.Exec(ctx, `DELETE FROM push_subscriptions WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return false, fmt.Errorf("delete push subscription: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// targetsFor loads the delivery data of the given accounts' subscriptions.
func targetsFor(ctx context.Context, q db.Querier, userIDs []uuid.UUID) (map[uuid.UUID][]target, error) {
	rows, err := q.Query(ctx, `
		SELECT s.id, s.user_id, s.endpoint, s.p256dh, s.auth
		FROM push_subscriptions s JOIN users u ON u.id = s.user_id
		WHERE s.user_id = ANY($1::uuid[]) AND `+validSession+` ORDER BY s.created_at, s.id`, userIDs)
	if err != nil {
		return nil, fmt.Errorf("load push subscriptions: %w", err)
	}
	defer rows.Close()
	out := map[uuid.UUID][]target{}
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.id, &t.userID, &t.endpoint, &t.p256dh, &t.auth); err != nil {
			return nil, fmt.Errorf("load push subscriptions: %w", err)
		}
		out[t.userID] = append(out[t.userID], t)
	}
	return out, rows.Err()
}

// recordSuccess notes a delivery the push service accepted.
func recordSuccess(ctx context.Context, q db.Querier, id uuid.UUID) error {
	_, err := q.Exec(ctx, `UPDATE push_subscriptions SET last_success_at = NOW(), failures = 0 WHERE id = $1`, id)
	return err
}

// recordFailure counts a failed delivery and removes a subscription that kept failing.
func recordFailure(ctx context.Context, q db.Querier, id uuid.UUID) error {
	if _, err := q.Exec(ctx, `UPDATE push_subscriptions SET failures = failures + 1 WHERE id = $1`, id); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `DELETE FROM push_subscriptions WHERE id = $1 AND failures >= $2`, id, maxFailures)
	return err
}

// recordGone removes a subscription the push service no longer knows (404, 410).
func recordGone(ctx context.Context, q db.Querier, id uuid.UUID) error {
	_, err := q.Exec(ctx, `DELETE FROM push_subscriptions WHERE id = $1`, id)
	return err
}

// preview reads and writes whether the account's pushes carry message text.
func preview(ctx context.Context, q db.Querier, userID uuid.UUID) (bool, error) {
	var on bool
	err := q.QueryRow(ctx, `SELECT push_preview FROM users WHERE id = $1`, userID).Scan(&on)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	return on, err
}

func setPreview(ctx context.Context, q db.Querier, userID uuid.UUID, on bool) error {
	_, err := q.Exec(ctx, `UPDATE users SET push_preview = $2 WHERE id = $1`, userID, on)
	return err
}

// loadPushPrefs reads the language and the preview choice of one member.
func loadPushPrefs(ctx context.Context, q db.Querier, user uuid.UUID) (locale string, preview bool, err error) {
	err = q.QueryRow(ctx, `SELECT locale, push_preview FROM users WHERE id = $1 AND disabled_at IS NULL`, user).Scan(&locale, &preview)
	return locale, preview, err
}
