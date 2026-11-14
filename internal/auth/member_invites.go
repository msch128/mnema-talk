package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/raid"
)

// What a member's invite is when the request leaves it open: a day, ten uses,
// each cut down to the bounds the admins set.
const (
	defaultMemberInviteHours = 24
	defaultMemberInviteUses  = 10
)

// MemberInviteLimits are the bounds the holders of Manage invites set for the
// invites members create.
type MemberInviteLimits struct {
	// MaxExpiresInHours is the longest an invite of a member may stay valid.
	MaxExpiresInHours int `json:"max_expires_in_hours" minimum:"1" maximum:"8760"`
	// MaxUses is the most uses one invite of a member may have.
	MaxUses int `json:"max_uses" minimum:"1" maximum:"1000"`
	// MaxActive is how many invites of one member may be valid at once.
	MaxActive int `json:"max_active" minimum:"1" maximum:"100"`
	// MaxPerHour is how many invites one member may create per hour.
	MaxPerHour int `json:"max_per_hour" minimum:"1" maximum:"100"`
}

// Validate refuses limits outside what the settings table accepts.
func (l MemberInviteLimits) Validate() error {
	switch {
	case l.MaxExpiresInHours < 1 || l.MaxExpiresInHours > 24*365:
		return httpx.ErrInvalidInput("max_expires_in_hours must be between 1 and 8760")
	case l.MaxUses < 1 || l.MaxUses > 1000:
		return httpx.ErrInvalidInput("max_uses must be between 1 and 1000")
	case l.MaxActive < 1 || l.MaxActive > 100:
		return httpx.ErrInvalidInput("max_active must be between 1 and 100")
	case l.MaxPerHour < 1 || l.MaxPerHour > 100:
		return httpx.ErrInvalidInput("max_per_hour must be between 1 and 100")
	}
	return nil
}

// LoadMemberInviteLimits reads the limits.
func LoadMemberInviteLimits(ctx context.Context, q db.Querier) (MemberInviteLimits, error) {
	var l MemberInviteLimits
	err := q.QueryRow(ctx, `
		SELECT member_invite_max_hours, member_invite_max_uses, member_invite_active_cap, member_invite_per_hour
		FROM instance_settings`).Scan(&l.MaxExpiresInHours, &l.MaxUses, &l.MaxActive, &l.MaxPerHour)
	if err != nil {
		return l, fmt.Errorf("load member invite limits: %w", err)
	}
	return l, nil
}

// SaveMemberInviteLimits stores validated limits.
func SaveMemberInviteLimits(ctx context.Context, q db.Querier, l MemberInviteLimits) error {
	if err := l.Validate(); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `
		UPDATE instance_settings SET member_invite_max_hours = $1, member_invite_max_uses = $2,
			member_invite_active_cap = $3, member_invite_per_hour = $4`,
		l.MaxExpiresInHours, l.MaxUses, l.MaxActive, l.MaxPerHour)
	if err != nil {
		return fmt.Errorf("save member invite limits: %w", err)
	}
	return nil
}

// CreateMemberInviteRequest is the body of POST /api/me/invites.
type CreateMemberInviteRequest struct {
	// ChannelID is the channel the joiner lands in; omitted: the default
	// channel. The member must see the channel and hold Create invites in it.
	ChannelID *uuid.UUID `json:"channel_id" format:"uuid" binding:"optional" extensions:"x-nullable"`
	// MaxUses is 10 (or the limit, if lower) when omitted; never unlimited.
	MaxUses *int `json:"max_uses" minimum:"1" maximum:"1000" binding:"optional" extensions:"x-nullable"`
	// ExpiresInHours is 24 (or the limit, if lower) when omitted; never unlimited.
	ExpiresInHours *int `json:"expires_in_hours" minimum:"1" maximum:"8760" binding:"optional" extensions:"x-nullable"`
}

// resolve applies the defaults and refuses values above the limits.
func (r CreateMemberInviteRequest) resolve(l MemberInviteLimits, now time.Time) (maxUses int, expires time.Time, err error) {
	hours, uses := min(defaultMemberInviteHours, l.MaxExpiresInHours), min(defaultMemberInviteUses, l.MaxUses)
	if r.ExpiresInHours != nil {
		hours = *r.ExpiresInHours
	}
	if r.MaxUses != nil {
		uses = *r.MaxUses
	}
	if err := validateMaxUses(&uses); err != nil {
		return 0, time.Time{}, err
	}
	if _, err := validateInviteExpiry(&hours); err != nil {
		return 0, time.Time{}, err
	}
	if hours > l.MaxExpiresInHours {
		return 0, time.Time{}, httpx.ErrInvalidInput(fmt.Sprintf("an invite can last at most %d hours", l.MaxExpiresInHours))
	}
	if uses > l.MaxUses {
		return 0, time.Time{}, httpx.ErrInvalidInput(fmt.Sprintf("an invite can have at most %d uses", l.MaxUses))
	}
	return uses, now.Add(time.Duration(hours) * time.Hour), nil
}

// MemberInviteList is what a member sees of their invites.
type MemberInviteList struct {
	// Invites are the member's own, newest first.
	Invites []Invite `json:"invites"`
	// Limits are the bounds new invites must stay within.
	Limits MemberInviteLimits `json:"limits"`
}

// ListOwnInvites returns the invites created_by owner with the limits.
func ListOwnInvites(ctx context.Context, q db.Querier, owner uuid.UUID) (*MemberInviteList, error) {
	limits, err := LoadMemberInviteLimits(ctx, q)
	if err != nil {
		return nil, err
	}
	invites, err := listInvitesWhere(ctx, q, "i.created_by = $1", owner)
	if err != nil {
		return nil, err
	}
	return &MemberInviteList{Invites: invites, Limits: limits}, nil
}

// MemberInviteAudit writes the audit entry in the transaction of the change.
type MemberInviteAudit func(tx pgx.Tx, inv *Invite) error

// CreateMemberInvite stores an invite of a member, within the limits. The
// caller has checked the permission and that the member may invite into
// channel. unlimited skips the active and per-hour caps (a member who already
// holds Manage invites could create without them). The member's rows are
// locked, so two requests cannot both pass the same cap.
func CreateMemberInvite(ctx context.Context, p *db.Pool, member uuid.UUID, req CreateMemberInviteRequest, unlimited bool, audited MemberInviteAudit) (*Invite, error) {
	var inv *Invite
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		uses, expires, err := admitMemberInvite(ctx, tx, member, req, unlimited)
		if err != nil {
			return err
		}
		if inv, err = storeMemberInvite(ctx, tx, member, req.ChannelID, uses, expires); err != nil {
			return err
		}
		if audited != nil {
			return audited(tx, inv)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return inv, nil
}

// admitMemberInvite takes the member's lock, then checks the request against
// the limits, a lockdown that pauses invites and, unless unlimited, the caps.
func admitMemberInvite(ctx context.Context, tx pgx.Tx, member uuid.UUID, req CreateMemberInviteRequest, unlimited bool) (int, time.Time, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('member-invite:' || $1::text, 0))`, member); err != nil {
		return 0, time.Time{}, fmt.Errorf("lock member invites: %w", err)
	}
	limits, err := LoadMemberInviteLimits(ctx, tx)
	if err != nil {
		return 0, time.Time{}, err
	}
	uses, expires, err := req.resolve(limits, time.Now())
	if err != nil {
		return 0, time.Time{}, err
	}
	if err := raid.CheckInvite(ctx, tx); err != nil {
		return 0, time.Time{}, err
	}
	if !unlimited {
		if err := checkMemberInviteCaps(ctx, tx, member, limits); err != nil {
			return 0, time.Time{}, err
		}
	}
	return uses, expires, nil
}

// storeMemberInvite inserts the invite with a fresh code. The caller's audit
// entry, written in the same transaction, is what the per-hour limit counts.
func storeMemberInvite(ctx context.Context, tx pgx.Tx, member uuid.UUID, channel *uuid.UUID, uses int, expires time.Time) (*Invite, error) {
	code, err := GenerateInviteCode()
	if err != nil {
		return nil, err
	}
	inv, err := insertInvite(ctx, tx, member, code, &uses, &expires, channel)
	if err != nil {
		return nil, mapInsertInviteError(err)
	}
	return inv, nil
}

// checkMemberInviteCaps refuses a new invite past the active cap or the
// hourly limit.
func checkMemberInviteCaps(ctx context.Context, tx pgx.Tx, member uuid.UUID, l MemberInviteLimits) error {
	var active, lastHour int
	err := tx.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM invites WHERE created_by = $1
				AND (expires_at IS NULL OR expires_at > NOW()) AND (max_uses IS NULL OR uses_count < max_uses)),
			(SELECT COUNT(*) FROM audit_log WHERE actor_id = $1 AND action = 'invite.member_create' AND created_at > NOW() - INTERVAL '1 hour')`,
		member).Scan(&active, &lastHour)
	if err != nil {
		return fmt.Errorf("count member invites: %w", err)
	}
	if active >= l.MaxActive {
		return httpx.ErrConflict(fmt.Sprintf("you already have %d active invites: revoke one first", l.MaxActive))
	}
	if lastHour >= l.MaxPerHour {
		e := httpx.NewAPIError(http.StatusTooManyRequests, httpx.CodeRateLimited, fmt.Sprintf("you can create %d invites per hour", l.MaxPerHour))
		e.RetryAfter = time.Hour
		return e
	}
	return nil
}

// DeleteOwnInvite revokes an invite the member created. Someone else's invite
// answers as an unknown one.
func DeleteOwnInvite(ctx context.Context, p *db.Pool, member, id uuid.UUID, audited MemberInviteAudit) error {
	return pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		inv, err := scanInvite(tx.QueryRow(ctx, `
			WITH i AS (DELETE FROM invites WHERE id = $1 AND created_by = $2 RETURNING *)
			SELECT `+inviteColumns+` FROM i JOIN users u ON u.id = i.created_by`, id, member))
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound("invite not found")
		}
		if err != nil {
			return fmt.Errorf("delete invite: %w", err)
		}
		if audited != nil {
			return audited(tx, inv)
		}
		return nil
	})
}
