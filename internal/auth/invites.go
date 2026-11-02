package auth

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/raid"
)

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
		inv.Weak = IsWeakInviteCode(inv.Code)
		invites = append(invites, inv)
	}
	return invites, rows.Err()
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
	var inv Invite
	err = p.QueryRow(ctx, `
		INSERT INTO invites (code, created_by, max_uses, expires_at) VALUES ($1, $2, $3, $4)
		RETURNING id, code, max_uses, uses_count, expires_at, created_at`,
		code, createdBy, maxUses, expiresAt).
		Scan(&inv.ID, &inv.Code, &inv.MaxUses, &inv.UsesCount, &inv.ExpiresAt, &inv.CreatedAt)
	if err != nil {
		return nil, mapInsertInviteError(err)
	}
	inv.Weak = IsWeakInviteCode(inv.Code)
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

// MountAdmin registers invite and account management; each route checks its
// own permission (RequireUser must already apply).
func (h *Handler) MountAdmin(r chi.Router) {
	invites := r.With(RequirePerm(h.Perms, perm.CreateInvites))
	invites.Get("/invites", httpx.Handle(h.listInvites))
	invites.Post("/invites", httpx.Handle(h.createInvite))
	invites.Patch("/invites/{id}", httpx.Handle(h.updateInvite))
	invites.Delete("/invites/{id}", httpx.Handle(h.deleteInvite))
	h.mountUserAdmin(r)
}

// listInvites handles GET /api/admin/invites.
//
// @Summary List invites
// @Description Requires the Manage invites permission (403 otherwise).
// @ID listInvites
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Success 200 {array} Invite "Invites, newest first."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage invites permission."
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
// @Description Requires the Manage invites permission (403 otherwise).
// @ID createInvite
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body CreateInviteRequest true "Request body."
// @Success 201 {object} Invite "Created invite."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage invites permission, or cross-origin request rejected."
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

// updateInvite handles PATCH /api/admin/invites/{id}.
//
// @Summary Change an invite's expiry or use limit
// @Description Requires the Manage invites permission (403 otherwise). Absent fields are kept; null removes the expiry or the limit. A max_uses below the uses already consumed is refused (400).
// @ID updateInvite
// @Tags Admin
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Resource ID." Format(uuid)
// @Param request body UpdateInviteRequest true "Request body."
// @Success 200 {object} Invite "Updated invite."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage invites permission, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/invites/{id} [patch]
func (h *Handler) updateInvite(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var req UpdateInviteRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	inv, err := UpdateInvite(r.Context(), h.Sessions.DB, id, req)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, inv)
	return nil
}

// deleteInvite handles DELETE /api/admin/invites/{id}.
//
// @Summary Delete an invite
// @Description Requires the Manage invites permission (403 otherwise).
// @ID deleteInvite
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Param id path string true "Resource ID." Format(uuid)
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage invites permission, or cross-origin request rejected."
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
	Code string `json:"code" pattern:"^[A-Za-z0-9_-]{12,64}$" binding:"optional"`
	// MaxUses is unlimited when null or omitted.
	MaxUses *int `json:"max_uses" minimum:"1" maximum:"1000" binding:"optional" extensions:"x-nullable"`
	// ExpiresInHours never expires when null or omitted.
	ExpiresInHours *int `json:"expires_in_hours" minimum:"1" maximum:"8760" binding:"optional" extensions:"x-nullable"`
}
