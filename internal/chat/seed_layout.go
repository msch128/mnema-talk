package chat

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
)

// seedRenames are the default categories and channels migration 0001 creates
// (in German, before any language is known) and their English counterparts.
var seedRenames = struct {
	categories map[string]string
	channels   map[string][2]string // old name -> new name, new topic
}{
	categories: map[string]string{"Text-Kanäle": "Text channels", "Voice-Hangouts": "Voice hangouts"},
	channels: map[string][2]string{
		"general":   {"general", "General discussion and chat"},
		"medien":    {"media", "Pictures, screenshots and clips"},
		"Lounge":    {"Lounge", "Open voice hangout"},
		"Gaming 4K": {"Gaming 4K", "Source-quality screen sharing and gaming hangout"},
	},
}

// LocalizeSeedLayout gives a brand-new installation English names for the
// default categories and channels when its first-run language is English. It
// only acts on an untouched start: a single account (the Owner just seeded), no
// messages, and exactly the four seeded channels under their German names, so
// existing data is never rewritten. It reports whether it renamed anything.
func LocalizeSeedLayout(ctx context.Context, p *db.Pool, language string) (bool, error) {
	if language != "en" {
		return false, nil
	}
	renamed := false
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		untouched, err := isUntouchedSeed(ctx, tx)
		if err != nil || !untouched {
			return err
		}
		for from, to := range seedRenames.categories {
			if _, err := tx.Exec(ctx, `UPDATE categories SET name = $2 WHERE name = $1`, from, to); err != nil {
				return fmt.Errorf("rename seeded category: %w", err)
			}
		}
		for from, to := range seedRenames.channels {
			if _, err := tx.Exec(ctx, `UPDATE channels SET name = $2, topic = $3 WHERE name = $1`, from, to[0], to[1]); err != nil {
				return fmt.Errorf("rename seeded channel: %w", err)
			}
		}
		renamed = true
		return nil
	})
	return renamed, err
}

func isUntouchedSeed(ctx context.Context, tx pgx.Tx) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `
		SELECT (SELECT COUNT(*) FROM users) = 1
		   AND (SELECT COUNT(*) FROM messages) = 0
		   AND (SELECT COUNT(*) FROM categories) = 2
		   AND (SELECT COUNT(*) FROM channels) = 4
		   AND (SELECT COUNT(*) FROM categories WHERE name IN ('Text-Kanäle', 'Voice-Hangouts')) = 2
		   AND (SELECT COUNT(*) FROM channels WHERE name IN ('general', 'medien', 'Lounge', 'Gaming 4K')) = 4`).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("check seeded layout: %w", err)
	}
	return ok, nil
}
