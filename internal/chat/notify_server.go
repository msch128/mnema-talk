package chat

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// ServerNotify is a member's server-wide default: the level channels and
// categories without a setting of their own follow, and whether @all and @here
// are ignored. Without a stored row it is "all" and nothing suppressed.
type ServerNotify struct {
	Level            NotifyLevel `json:"level" enums:"all,mentions,mute"`
	MutedUntil       *time.Time  `json:"muted_until" format:"date-time" extensions:"x-nullable"`
	SuppressEveryone bool        `json:"suppress_everyone"`
}

// GetServerNotify returns the member's server default and whether they
// suppress @all and @here. A member without a row gets the plain default; an
// expired mute reads as "all".
func GetServerNotify(ctx context.Context, q db.Querier, userID uuid.UUID) (ServerNotify, error) {
	var s ServerNotify
	err := q.QueryRow(ctx, `
		SELECT CASE WHEN expired THEN 'all' ELSE notify_level END,
		       CASE WHEN expired THEN NULL ELSE muted_until END, suppress_everyone
		FROM (SELECT *, notify_level = 'mute' AND muted_until IS NOT NULL AND muted_until <= NOW() AS expired
		      FROM server_notify WHERE user_id = $1) t`, userID).Scan(&s.Level, &s.MutedUntil, &s.SuppressEveryone)
	if errors.Is(err, pgx.ErrNoRows) {
		return ServerNotify{Level: NotifyAll}, nil
	}
	return s, err
}

// SetServerNotify stores the member's server default. inherit is not a server
// level (there is nothing above it); until ends a mute.
func SetServerNotify(ctx context.Context, p *db.Pool, userID uuid.UUID, in ServerNotify) error {
	if in.Level == NotifyInherit || !in.Level.valid() {
		return httpx.ErrInvalidInput("level must be all, mentions or mute")
	}
	if err := validateSetting(ctx, p, in.Level, in.MutedUntil); err != nil {
		return err
	}
	_, err := p.Exec(ctx, `
		INSERT INTO server_notify (user_id, notify_level, muted_until, suppress_everyone) VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id) DO UPDATE
		SET notify_level = EXCLUDED.notify_level, muted_until = EXCLUDED.muted_until,
		    suppress_everyone = EXCLUDED.suppress_everyone`,
		userID, in.Level, in.MutedUntil, in.SuppressEveryone)
	return err
}
