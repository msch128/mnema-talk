package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

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
		err := tx.QueryRow(ctx, `
			SELECT id, code, max_uses, uses_count, expires_at, created_at
			FROM invites WHERE id = $1 FOR UPDATE`, id).
			Scan(&inv.ID, &inv.Code, &inv.MaxUses, &inv.UsesCount, &inv.ExpiresAt, &inv.CreatedAt)
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
