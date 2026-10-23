package chat

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
)

// seedLayoutJSON holds the default category and channel names per language.
// Migration 0001 seeds the baseline language (German) before any language is
// known; a fresh installation in another language is renamed from it.
//
//go:embed seed_layout.json
var seedLayoutJSON []byte

const seedBaseline = "de"

type seedChannel struct {
	Name  string `json:"name"`
	Topic string `json:"topic"`
}

type seedLayout struct {
	Categories map[string]string      `json:"categories"`
	Channels   map[string]seedChannel `json:"channels"`
}

func loadSeedLayouts() map[string]seedLayout {
	var out map[string]seedLayout
	if err := json.Unmarshal(seedLayoutJSON, &out); err != nil {
		panic("chat: seed_layout.json: " + err.Error())
	}
	return out
}

var seedLayouts = loadSeedLayouts()

// LocalizeSeedLayout gives a brand-new installation the default category and
// channel names of its first-run language when that is not the baseline. It
// only acts on an untouched start: a single account (the Owner just seeded), no
// messages, and exactly the four seeded channels under their baseline names, so
// existing data is never rewritten. It reports whether it renamed anything.
func LocalizeSeedLayout(ctx context.Context, p *db.Pool, language string) (bool, error) {
	target, ok := seedLayouts[language]
	if !ok || language == seedBaseline {
		return false, nil
	}
	base := seedLayouts[seedBaseline]
	renamed := false
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		untouched, err := isUntouchedSeed(ctx, tx, base)
		if err != nil || !untouched {
			return err
		}
		for key, from := range base.Categories {
			if _, err := tx.Exec(ctx, `UPDATE categories SET name = $2 WHERE name = $1`, from, target.Categories[key]); err != nil {
				return fmt.Errorf("rename seeded category: %w", err)
			}
		}
		for key, from := range base.Channels {
			to := target.Channels[key]
			if _, err := tx.Exec(ctx, `UPDATE channels SET name = $2, topic = $3 WHERE name = $1`, from.Name, to.Name, to.Topic); err != nil {
				return fmt.Errorf("rename seeded channel: %w", err)
			}
		}
		renamed = true
		return nil
	})
	return renamed, err
}

func isUntouchedSeed(ctx context.Context, tx pgx.Tx, base seedLayout) (bool, error) {
	categories, channels := make([]string, 0, len(base.Categories)), make([]string, 0, len(base.Channels))
	for _, name := range base.Categories {
		categories = append(categories, name)
	}
	for _, c := range base.Channels {
		channels = append(channels, c.Name)
	}
	var ok bool
	err := tx.QueryRow(ctx, `
		SELECT (SELECT COUNT(*) FROM users) = 1
		   AND (SELECT COUNT(*) FROM messages) = 0
		   AND (SELECT COUNT(*) FROM categories) = $1
		   AND (SELECT COUNT(*) FROM channels) = $2
		   AND (SELECT COUNT(*) FROM categories WHERE name = ANY($3)) = $1
		   AND (SELECT COUNT(*) FROM channels WHERE name = ANY($4)) = $2`,
		len(categories), len(channels), categories, channels).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("check seeded layout: %w", err)
	}
	return ok, nil
}
