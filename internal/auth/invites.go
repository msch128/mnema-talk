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
	ID        uuid.UUID  `json:"id"`
	Code      string     `json:"code"`
	MaxUses   *int       `json:"max_uses"`
	UsesCount int        `json:"uses_count"`
	ExpiresAt *time.Time `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
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

func (h *Handler) listInvites(w http.ResponseWriter, r *http.Request) error {
	invites, err := ListInvites(r.Context(), h.Sessions.DB)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, invites)
	return nil
}

func (h *Handler) createInvite(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Code           string `json:"code"`
		MaxUses        *int   `json:"max_uses"`
		ExpiresInHours *int   `json:"expires_in_hours"`
	}
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
