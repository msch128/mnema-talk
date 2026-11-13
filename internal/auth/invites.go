package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/raid"
)

// Invite is an invite code as the admin list shows it.
type Invite struct {
	ID        uuid.UUID  `json:"id" format:"uuid"`
	Code      string     `json:"code"`
	MaxUses   *int       `json:"max_uses" extensions:"x-nullable"`
	UsesCount int        `json:"uses_count"`
	ExpiresAt *time.Time `json:"expires_at" format:"date-time" extensions:"x-nullable"`
	CreatedAt time.Time  `json:"created_at" format:"date-time"`
	// Weak is true for a custom code that is easy to guess (too short, a common
	// word). New codes like that are refused and old ones no longer work for
	// sign-up; regenerate them.
	Weak bool `json:"weak"`
	// ChannelID is the channel the invite lands the joiner in; null: the
	// default channel. Only invites created from a channel's menu have one.
	ChannelID *uuid.UUID `json:"channel_id" format:"uuid" extensions:"x-nullable"`
	// CreatedByID and CreatedByName say who created the invite.
	CreatedByID   uuid.UUID `json:"created_by_id" format:"uuid"`
	CreatedByName string    `json:"created_by_name"`
}

// inviteColumns are the columns of an Invite, read from invites i joined with
// its creator u (inviteFrom).
const (
	inviteColumns = `i.id, i.code, i.max_uses, i.uses_count, i.expires_at, i.created_at, i.channel_id, i.created_by, u.display_name`
	inviteFrom    = `FROM invites i JOIN users u ON u.id = i.created_by`
)

// scanInvite reads one row of inviteColumns.
func scanInvite(row pgx.Row) (*Invite, error) {
	var inv Invite
	err := row.Scan(&inv.ID, &inv.Code, &inv.MaxUses, &inv.UsesCount, &inv.ExpiresAt, &inv.CreatedAt, &inv.ChannelID, &inv.CreatedByID, &inv.CreatedByName)
	if err != nil {
		return nil, err
	}
	inv.Weak = IsWeakInviteCode(inv.Code)
	return &inv, nil
}

// listInvitesWhere reads the invites matching where (a condition on i, with
// args), newest first.
func listInvitesWhere(ctx context.Context, q db.Querier, where string, args ...any) ([]Invite, error) {
	rows, err := q.Query(ctx, `SELECT `+inviteColumns+` `+inviteFrom+` WHERE `+where+` ORDER BY i.created_at DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("query invites: %w", err)
	}
	defer rows.Close()
	invites := make([]Invite, 0)
	for rows.Next() {
		inv, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		invites = append(invites, *inv)
	}
	return invites, rows.Err()
}

// maxInviteCodeLength is the longest custom code. The shortest is
// minCustomCodeLength; the pattern tag of CreateInviteRequest.Code repeats both
// numbers (a test keeps them in step).
const maxInviteCodeLength = 64

var inviteCodePattern = regexp.MustCompile(fmt.Sprintf(`^[A-Za-z0-9_-]{%d,%d}$`, minCustomCodeLength, maxInviteCodeLength))

// inviteAlphabet avoids look-alike characters so codes can be read aloud.
const inviteAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

// GenerateInviteCode returns a random 12-character code (~59 bits).
func GenerateInviteCode() (string, error) {
	for {
		code, err := randomInviteCode()
		// A random code that happens to spell a common word is not issued.
		if err != nil || !IsWeakInviteCode(code) {
			return code, err
		}
	}
}

func randomInviteCode() (string, error) {
	b := make([]byte, 12)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(inviteAlphabet))))
		if err != nil {
			return "", err
		}
		b[i] = inviteAlphabet[n.Int64()]
	}
	return string(b), nil
}

// ListInvites returns every invite, newest first, weak legacy codes flagged.
func ListInvites(ctx context.Context, p *db.Pool) ([]Invite, error) {
	return listInvitesWhere(ctx, p, "TRUE")
}

func validateOrGenerateCode(code string) (string, error) {
	if code == "" {
		return GenerateInviteCode()
	}
	if !inviteCodePattern.MatchString(code) {
		return "", httpx.ErrInvalidInput(fmt.Sprintf("code must be %d-%d characters: letters, digits, '_' or '-'", minCustomCodeLength, maxInviteCodeLength))
	}
	if err := checkCustomCodeStrength(code); err != nil {
		return "", err
	}
	return code, nil
}

func validateMaxUses(maxUses *int) error {
	if maxUses != nil && (*maxUses < 1 || *maxUses > 1000) {
		return httpx.ErrInvalidInput("max_uses must be between 1 and 1000")
	}
	return nil
}

func validateInviteExpiry(expiresInHours *int) (*time.Time, error) {
	if expiresInHours == nil {
		return nil, nil
	}
	if *expiresInHours < 1 || *expiresInHours > 24*365 {
		return nil, httpx.ErrInvalidInput("expires_in_hours must be between 1 and 8760")
	}
	t := time.Now().Add(time.Duration(*expiresInHours) * time.Hour)
	return &t, nil
}

func mapInsertInviteError(err error) error {
	if err == nil {
		return nil
	}
	if db.IsUniqueViolation(err) {
		return httpx.ErrConflict("invite code already exists")
	}
	return fmt.Errorf("insert invite: %w", err)
}

// CreateInvite stores an invite. An empty code is generated randomly.
func CreateInvite(ctx context.Context, p *db.Pool, createdBy uuid.UUID, rawCode string, maxUses *int, expiresInHours *int) (*Invite, error) {
	code, err := validateOrGenerateCode(rawCode)
	if err != nil {
		return nil, err
	}
	if err := validateMaxUses(maxUses); err != nil {
		return nil, err
	}
	expiresAt, err := validateInviteExpiry(expiresInHours)
	if err != nil {
		return nil, err
	}
	if err := raid.CheckInvite(ctx, p); err != nil {
		return nil, err
	}
	inv, err := insertInvite(ctx, p, createdBy, code, maxUses, expiresAt, nil)
	if err != nil {
		return nil, mapInsertInviteError(err)
	}
	return inv, nil
}

// insertInvite stores an invite and reads it back with its creator.
func insertInvite(ctx context.Context, q db.Querier, createdBy uuid.UUID, code string, maxUses *int, expiresAt *time.Time, channel *uuid.UUID) (*Invite, error) {
	return scanInvite(q.QueryRow(ctx, `
		WITH i AS (
			INSERT INTO invites (code, created_by, max_uses, expires_at, channel_id) VALUES ($1, $2, $3, $4, $5)
			RETURNING *)
		SELECT `+inviteColumns+` FROM i JOIN users u ON u.id = i.created_by`,
		code, createdBy, maxUses, expiresAt, channel))
}

// DeleteInvite revokes an invite; accounts that registered with it stay.
func DeleteInvite(ctx context.Context, p *db.Pool, id uuid.UUID) error {
	tag, err := p.Exec(ctx, `DELETE FROM invites WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete invite: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return httpx.ErrNotFound("invite not found")
	}
	return nil
}

// CreateInviteRequest is the body of POST /api/admin/invites.
type CreateInviteRequest struct {
	// Code is generated (12 characters) when omitted or empty.
	Code string `json:"code" pattern:"^[A-Za-z0-9_-]{12,64}$" binding:"optional"`
	// MaxUses is unlimited when null or omitted.
	MaxUses *int `json:"max_uses" minimum:"1" maximum:"1000" binding:"optional" extensions:"x-nullable"`
	// ExpiresInHours never expires when null or omitted.
	ExpiresInHours *int `json:"expires_in_hours" minimum:"1" maximum:"8760" binding:"optional" extensions:"x-nullable"`
}

// maxInviteExpiry is how far ahead an invite may expire (as at creation).
const maxInviteExpiry = 24 * 365 * time.Hour

// UpdateInvite changes the limits of an invite. Only the fields present in
// req are touched: a null expires_at removes the expiry, a null (or 0)
// max_uses makes the invite unlimited. A new max_uses below the uses already
// consumed is refused rather than silently exhausting the invite; equal to
// the uses is allowed (the invite is then used up). The invite row is locked,
// as at sign-up, so a concurrent registration cannot slip past the check.
func UpdateInvite(ctx context.Context, p *db.Pool, id uuid.UUID, req UpdateInviteRequest) (*Invite, error) {
	if err := req.validate(time.Now()); err != nil {
		return nil, err
	}
	var inv Invite
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		locked, err := scanInvite(tx.QueryRow(ctx, `SELECT `+inviteColumns+` `+inviteFrom+` WHERE i.id = $1 FOR UPDATE OF i`, id))
		if err == nil {
			inv = *locked
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound("invite not found")
		}
		if err != nil {
			return fmt.Errorf("lock invite: %w", err)
		}
		if err := req.apply(&inv); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE invites SET max_uses = $2, expires_at = $3 WHERE id = $1`, id, inv.MaxUses, inv.ExpiresAt); err != nil {
			return fmt.Errorf("update invite: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	inv.Weak = IsWeakInviteCode(inv.Code)
	return &inv, nil
}

// validate checks what does not depend on the stored invite; 0 uses means
// unlimited and is normalised to nil.
func (r *UpdateInviteRequest) validate(now time.Time) error {
	if !r.setMaxUses && !r.setExpiresAt {
		return httpx.ErrInvalidInput("nothing to change: send expires_at or max_uses")
	}
	if r.MaxUses != nil && *r.MaxUses == 0 {
		r.MaxUses = nil
	}
	if err := validateMaxUses(r.MaxUses); err != nil {
		return err
	}
	return validateNewExpiry(r.ExpiresAt, now)
}

// apply puts the sent fields on the locked invite.
func (r UpdateInviteRequest) apply(inv *Invite) error {
	if r.setMaxUses {
		if r.MaxUses != nil && *r.MaxUses < inv.UsesCount {
			return httpx.ErrInvalidInput(fmt.Sprintf("max_uses must be at least the %d uses already consumed", inv.UsesCount))
		}
		inv.MaxUses = r.MaxUses
	}
	if r.setExpiresAt {
		inv.ExpiresAt = r.ExpiresAt
	}
	return nil
}

func validateNewExpiry(t *time.Time, now time.Time) error {
	if t == nil {
		return nil
	}
	if !t.After(now) {
		return httpx.ErrInvalidInput("expires_at must be in the future")
	}
	if t.After(now.Add(maxInviteExpiry)) {
		return httpx.ErrInvalidInput("expires_at must be within 8760 hours from now")
	}
	return nil
}

// UpdateInviteRequest is the body of PATCH /api/admin/invites/{id}. A field
// that is absent is kept; an explicit null removes the expiry or the limit.
type UpdateInviteRequest struct {
	// ExpiresAt is a future time (within 8760 hours); null removes the expiry.
	ExpiresAt *time.Time `json:"expires_at" format:"date-time" binding:"optional" extensions:"x-nullable"`
	// MaxUses is at least the uses consumed so far; null (or 0) makes the invite unlimited.
	MaxUses *int `json:"max_uses" minimum:"0" maximum:"1000" binding:"optional" extensions:"x-nullable"`

	setExpiresAt, setMaxUses bool
}

// UnmarshalJSON decodes the request strictly (unknown fields are refused, as
// httpx.DecodeJSON does) and records which fields were sent, null included.
func (r *UpdateInviteRequest) UnmarshalJSON(data []byte) error {
	type plain UpdateInviteRequest
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode((*plain)(r)); err != nil {
		return err
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	_, r.setExpiresAt = probe["expires_at"]
	_, r.setMaxUses = probe["max_uses"]
	return nil
}
