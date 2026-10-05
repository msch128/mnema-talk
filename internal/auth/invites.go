package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

type Invite struct {
	ID        uuid.UUID  `json:"id" format:"uuid"`
	Code      string     `json:"code"`
	MaxUses   *int       `json:"max_uses" extensions:"x-nullable"`
	UsesCount int        `json:"uses_count"`
	ExpiresAt *time.Time `json:"expires_at" format:"date-time" extensions:"x-nullable"`
	CreatedAt time.Time  `json:"created_at" format:"date-time"`
}

var inviteCodePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{6,64}$`)

// inviteAlphabet avoids look-alike characters so codes can be read aloud.
const inviteAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

// GenerateInviteCode returns a random 12-character code (~59 bits).
func GenerateInviteCode() (string, error) {
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

func ListInvites(ctx context.Context, p *db.Pool) ([]Invite, error) {
	rows, err := p.Query(ctx, `
		SELECT id, code, max_uses, uses_count, expires_at, created_at
		FROM invites ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query invites: %w", err)
	}
	defer rows.Close()
	invites := make([]Invite, 0)
	for rows.Next() {
		var inv Invite
		if err := rows.Scan(&inv.ID, &inv.Code, &inv.MaxUses, &inv.UsesCount, &inv.ExpiresAt, &inv.CreatedAt); err != nil {
			return nil, err
		}
		invites = append(invites, inv)
	}
	return invites, rows.Err()
}

// CreateInvite stores an invite. An empty code is generated randomly.
func CreateInvite(ctx context.Context, p *db.Pool, createdBy uuid.UUID, code string, maxUses *int, expiresInHours *int) (*Invite, error) {
	if code == "" {
		var err error
		if code, err = GenerateInviteCode(); err != nil {
			return nil, err
		}
	} else if !inviteCodePattern.MatchString(code) {
		return nil, httpx.ErrInvalidInput("code must be 6-64 characters: letters, digits, '_' or '-'")
	}
	if maxUses != nil && (*maxUses < 1 || *maxUses > 1000) {
		return nil, httpx.ErrInvalidInput("max_uses must be between 1 and 1000")
	}
	var expiresAt *time.Time
	if expiresInHours != nil {
		if *expiresInHours < 1 || *expiresInHours > 24*365 {
			return nil, httpx.ErrInvalidInput("expires_in_hours must be between 1 and 8760")
		}
		t := time.Now().Add(time.Duration(*expiresInHours) * time.Hour)
		expiresAt = &t
	}
	var inv Invite
	err := p.QueryRow(ctx, `
		INSERT INTO invites (code, created_by, max_uses, expires_at) VALUES ($1, $2, $3, $4)
		RETURNING id, code, max_uses, uses_count, expires_at, created_at`,
		code, createdBy, maxUses, expiresAt).
		Scan(&inv.ID, &inv.Code, &inv.MaxUses, &inv.UsesCount, &inv.ExpiresAt, &inv.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, httpx.ErrConflict("invite code already exists")
		}
		return nil, fmt.Errorf("insert invite: %w", err)
	}
	return &inv, nil
}

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

// MountAdmin registers invite management (RequireAdmin must already apply).
func (h *Handler) MountAdmin(r chi.Router) {
	r.Get("/invites", httpx.Handle(h.listInvites))
	r.Post("/invites", httpx.Handle(h.createInvite))
	r.Delete("/invites/{id}", httpx.Handle(h.deleteInvite))
	h.mountUserAdmin(r)
}

// listInvites handles GET /api/admin/invites.
//
// @Summary List invites
// @Description Requires role admin (403 otherwise).
// @ID listInvites
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Success 200 {array} Invite "Invites, newest first."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/invites [get]
func (h *Handler) listInvites(w http.ResponseWriter, r *http.Request) error {
	invites, err := ListInvites(r.Context(), h.Sessions.DB)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, invites)
	return nil
}

// createInvite handles POST /api/admin/invites.
//
// @Summary Create an invite
// @Description Requires role admin (403 otherwise).
// @ID createInvite
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body CreateInviteRequest true "Request body."
// @Success 201 {object} Invite "Created invite."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the resource already exists."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/invites [post]
func (h *Handler) createInvite(w http.ResponseWriter, r *http.Request) error {
	var req CreateInviteRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	inv, err := CreateInvite(r.Context(), h.Sessions.DB, UserFrom(r.Context()).ID, req.Code, req.MaxUses, req.ExpiresInHours)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, inv)
	return nil
}

// deleteInvite handles DELETE /api/admin/invites/{id}.
//
// @Summary Delete an invite
// @Description Requires role admin (403 otherwise).
// @ID deleteInvite
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Param id path string true "Resource ID." Format(uuid)
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/invites/{id} [delete]
func (h *Handler) deleteInvite(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := DeleteInvite(r.Context(), h.Sessions.DB, id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// CreateInviteRequest is the body of POST /api/admin/invites.
type CreateInviteRequest struct {
	// Code is generated (12 characters) when omitted or empty.
	Code string `json:"code" pattern:"^[A-Za-z0-9_-]{6,64}$" binding:"optional"`
	// MaxUses is unlimited when null or omitted.
	MaxUses *int `json:"max_uses" minimum:"1" maximum:"1000" binding:"optional" extensions:"x-nullable"`
	// ExpiresInHours never expires when null or omitted.
	ExpiresInHours *int `json:"expires_in_hours" minimum:"1" maximum:"8760" binding:"optional" extensions:"x-nullable"`
}
