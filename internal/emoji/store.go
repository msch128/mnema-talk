package emoji

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// Emoji is one custom emoji as clients see it.
type Emoji struct {
	ID   uuid.UUID `json:"id" format:"uuid"`
	Name string    `json:"name" pattern:"^[a-z0-9_]{2,32}$"`
	// Animated emoji are GIFs and have a still frame for reduced motion.
	Animated bool `json:"animated"`
	// URL serves the image; it changes with the content (the v parameter), so
	// it may be cached for good.
	URL string `json:"url"`
	// StillURL is the first frame (animated emoji only).
	StillURL  string    `json:"still_url,omitempty" binding:"optional"`
	CreatedAt time.Time `json:"created_at"`
}

// List is the response of GET /api/emoji.
type List struct {
	Emoji []Emoji `json:"emoji"`
	// Limit is how many custom emoji the instance allows (EMOJI_MAX).
	Limit int `json:"limit" minimum:"0"`
}

// row is a custom_emoji record.
type row struct {
	id          uuid.UUID
	name        string
	contentType string
	animated    bool
	key         string
	stillKey    *string
	hash        string
	createdAt   time.Time
}

const columns = `id, name, content_type, animated, s3_key, still_s3_key, content_hash, created_at`

func scanRow(r pgx.Row) (row, error) {
	var e row
	err := r.Scan(&e.id, &e.name, &e.contentType, &e.animated, &e.key, &e.stillKey, &e.hash, &e.createdAt)
	return e, err
}

func (e row) public() Emoji {
	out := Emoji{ID: e.id, Name: e.name, Animated: e.animated, CreatedAt: e.createdAt,
		URL: fmt.Sprintf("/api/emoji/%s/image?v=%s", e.id, e.hash[:16])}
	if e.stillKey != nil {
		out.StillURL = fmt.Sprintf("/api/emoji/%s/still?v=%s", e.id, e.hash[:16])
	}
	return out
}

func (h *Handler) list(ctx context.Context) ([]Emoji, error) {
	rows, err := h.DB.Query(ctx, `SELECT `+columns+` FROM custom_emoji ORDER BY name`)
	if err != nil {
		return nil, err
	}
	all, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) { return scanRow(r) })
	if err != nil {
		return nil, err
	}
	out := make([]Emoji, len(all))
	for i, e := range all {
		out[i] = e.public()
	}
	return out, nil
}

func (h *Handler) get(ctx context.Context, id uuid.UUID) (row, error) {
	e, err := scanRow(h.DB.QueryRow(ctx, `SELECT `+columns+` FROM custom_emoji WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return row{}, httpx.ErrNotFound("emoji not found")
	}
	return e, err
}

func errNameTaken() error { return httpx.ErrConflict("an emoji with this name already exists") }

// emojiLockKey serialises the count check of concurrent uploads.
const emojiLockKey = 0x656d6f6a69

// insert adds the emoji unless the instance is full or the name is taken.
func (h *Handler) insert(ctx context.Context, e row, size int, by uuid.UUID) error {
	return pgx.BeginFunc(ctx, h.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, emojiLockKey); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM custom_emoji`).Scan(&n); err != nil {
			return err
		}
		if n >= h.Max {
			return httpx.ErrConflict(fmt.Sprintf("the limit of %d custom emoji is reached", h.Max))
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO custom_emoji (id, name, content_type, animated, s3_key, still_s3_key, size_bytes, content_hash, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			e.id, e.name, e.contentType, e.animated, e.key, e.stillKey, size, e.hash, by)
		if db.IsUniqueViolation(err) {
			return errNameTaken()
		}
		return err
	})
}

func (h *Handler) rename(ctx context.Context, id uuid.UUID, name string) (row, error) {
	e, err := scanRow(h.DB.QueryRow(ctx, `UPDATE custom_emoji SET name = $2 WHERE id = $1 RETURNING `+columns, id, name))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return row{}, httpx.ErrNotFound("emoji not found")
	case db.IsUniqueViolation(err):
		return row{}, errNameTaken()
	}
	return e, err
}

func (h *Handler) remove(ctx context.Context, id uuid.UUID) (row, error) {
	e, err := scanRow(h.DB.QueryRow(ctx, `DELETE FROM custom_emoji WHERE id = $1 RETURNING `+columns, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return row{}, httpx.ErrNotFound("emoji not found")
	}
	return e, err
}
