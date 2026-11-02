package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
)

// VanityCodePrefix starts the invite code of a sign-up through the vanity
// link: "~<slug>". A real invite code is letters, digits, "_" and "-", so the
// two never collide. Whether the link takes sign-ups is the Owner's setting
// (instance_settings.vanity_accepts_signups); off, the code is simply invalid.
const VanityCodePrefix = "~"

// vanitySlug returns the slug of a vanity sign-up code.
func vanitySlug(code string) (string, bool) {
	slug, ok := strings.CutPrefix(code, VanityCodePrefix)
	return strings.ToLower(slug), ok
}

// checkCodeKnown is the cheap check before a password is hashed: the code
// names an invite, or a vanity link that takes sign-ups.
func checkCodeKnown(ctx context.Context, p *db.Pool, code string) error {
	query, arg := `SELECT EXISTS (SELECT 1 FROM invites WHERE code = $1)`, code
	if slug, ok := vanitySlug(code); ok {
		query, arg = vanityQuery, slug
	}
	var known bool
	if err := p.QueryRow(ctx, query, arg).Scan(&known); err != nil {
		return fmt.Errorf("invite lookup: %w", err)
	}
	if !known {
		return ErrInvalidInvite
	}
	return nil
}

const vanityQuery = `SELECT EXISTS (SELECT 1 FROM instance_settings WHERE vanity_slug = $1 AND vanity_accepts_signups)`

// claimCode claims the invite a code names, or confirms the vanity link under
// a share lock (so switching sign-ups off waits for, or beats, a sign-up in
// flight). The vanity link has no invite row: the result is nil.
func claimCode(ctx context.Context, tx pgx.Tx, code string) (*uuid.UUID, error) {
	if slug, ok := vanitySlug(code); ok {
		var accepted bool
		err := tx.QueryRow(ctx, `SELECT TRUE FROM instance_settings WHERE vanity_slug = $1 AND vanity_accepts_signups FOR SHARE`, slug).Scan(&accepted)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, ErrInvalidInvite
			}
			return nil, fmt.Errorf("vanity lookup: %w", err)
		}
		return nil, nil
	}
	id, err := claimInvite(ctx, tx, code)
	if err != nil {
		return nil, err
	}
	return &id, nil
}
