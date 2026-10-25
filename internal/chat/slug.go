package chat

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"golang.org/x/text/unicode/norm"
)

// MaxSlugLength is the longest channel slug.
const MaxSlugLength = 60

// fallbackSlug is the slug of a name without a single usable character.
const fallbackSlug = "channel"

// slugLockKey serialises slug assignment (a transaction-scoped advisory lock),
// so two concurrent creates never race for the same free slug.
const slugLockKey = 7_301_265_904

var (
	slugReplacer = strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss", "æ", "ae", "œ", "oe", "ø", "o", "ł", "l", "đ", "d", "ð", "d", "þ", "th")
	slugRuns     = regexp.MustCompile(`[^a-z0-9]+`)
	uuidShaped   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	slugSuffixed = regexp.MustCompile(`^-[0-9]+$`)
)

// Slugify turns a channel name into the base of its slug: lowercase ASCII
// [a-z0-9-], umlauts spelled out (ä becomes ae), other accents stripped,
// everything else (emoji, punctuation, spaces) a single dash, at most
// MaxSlugLength characters. A name without a usable character gives "channel".
// A result that looks like a UUID gets a prefix, so a slug can never shadow a
// legacy /c/<uuid> link.
func Slugify(name string) string {
	s := slugReplacer.Replace(strings.ToLower(name))
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		if !unicode.Is(unicode.Mn, r) {
			b.WriteRune(r)
		}
	}
	s = strings.Trim(slugRuns.ReplaceAllString(b.String(), "-"), "-")
	s = strings.Trim(truncate(s, MaxSlugLength), "-")
	if s == "" {
		return fallbackSlug
	}
	if uuidShaped.MatchString(s) {
		return fallbackSlug + "-" + s
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// slugCandidate is the n-th choice for base: base, base-2, base-3 ... the
// base is shortened so the result stays within MaxSlugLength.
func slugCandidate(base string, n int) string {
	if n <= 1 {
		return base
	}
	suffix := "-" + strconv.Itoa(n)
	return strings.Trim(truncate(base, MaxSlugLength-len(suffix)), "-") + suffix
}

// keepsSlug reports whether current is still a fitting slug for base: base
// itself or base with a collision suffix. A rename that changes only case or
// punctuation keeps the slug, so it does not grow the history.
func keepsSlug(current, base string) bool {
	if current == base {
		return true
	}
	rest, ok := strings.CutPrefix(current, base)
	return ok && slugSuffixed.MatchString(rest)
}

// assignSlug gives the channel the slug for name inside tx: a free one (the
// base, else base-2, base-3 ...) or one of its own earlier slugs. Slugs of
// other channels, current or former, are never taken. The channel keeps its
// slug when the name still fits it.
func assignSlug(ctx context.Context, tx pgx.Tx, id uuid.UUID, name string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, slugLockKey); err != nil {
		return fmt.Errorf("lock slugs: %w", err)
	}
	var current *string
	if err := tx.QueryRow(ctx, `SELECT slug FROM channels WHERE id = $1`, id).Scan(&current); err != nil {
		return fmt.Errorf("read slug: %w", err)
	}
	base := Slugify(name)
	if current != nil && keepsSlug(*current, base) {
		return nil
	}
	slug, err := freeSlug(ctx, tx, id, base)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE channels SET slug = $2 WHERE id = $1`, id, slug); err != nil {
		return fmt.Errorf("set slug: %w", err)
	}
	return nil
}

// freeSlug picks the first candidate nobody else holds or held, and records
// it in the channel's history.
func freeSlug(ctx context.Context, tx pgx.Tx, id uuid.UUID, base string) (string, error) {
	for n := 1; n <= 10_000; n++ {
		cand := slugCandidate(base, n)
		var owner uuid.UUID
		err := tx.QueryRow(ctx, `SELECT channel_id FROM channel_slugs WHERE slug = $1`, cand).Scan(&owner)
		if errors.Is(err, pgx.ErrNoRows) {
			if _, err := tx.Exec(ctx, `INSERT INTO channel_slugs (slug, channel_id) VALUES ($1, $2)`, cand, id); err != nil {
				return "", fmt.Errorf("record slug: %w", err)
			}
			return cand, nil
		}
		if err != nil {
			return "", fmt.Errorf("check slug: %w", err)
		}
		if owner == id {
			return cand, nil
		}
	}
	return "", errors.New("no free channel slug")
}

// AssignSlugs gives each channel its slug inside tx, in order (an import
// creating many channels at once).
func AssignSlugs(ctx context.Context, tx pgx.Tx, ids []uuid.UUID, names []string) error {
	for i, id := range ids {
		if err := assignSlug(ctx, tx, id, names[i]); err != nil {
			return err
		}
	}
	return nil
}

// FillSlugs loads the slug and the former slugs of ch.
func FillSlugs(ctx context.Context, q db.Querier, ch *Channel) error {
	rows, err := q.Query(ctx, `
		SELECT s.slug, s.slug IS NOT DISTINCT FROM c.slug
		FROM channel_slugs s JOIN channels c ON c.id = s.channel_id
		WHERE s.channel_id = $1 ORDER BY s.created_at, s.slug`, ch.ID)
	if err != nil {
		return fmt.Errorf("load slugs: %w", err)
	}
	defer rows.Close()
	ch.PreviousSlugs = []string{}
	for rows.Next() {
		var slug string
		var current bool
		if err := rows.Scan(&slug, &current); err != nil {
			return err
		}
		if current {
			ch.Slug = slug
		} else {
			ch.PreviousSlugs = append(ch.PreviousSlugs, slug)
		}
	}
	return rows.Err()
}
