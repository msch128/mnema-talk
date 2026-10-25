package chat

import (
	"context"
	"errors"
	"fmt"
	"regexp"
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

// MaxFormerSlugs is how many former slugs a channel keeps reserved; older ones
// are dropped and free again (their old links stop resolving).
const MaxFormerSlugs = 10

// tempSlugPattern is the fixed slug of a temporary Talk: "talk-" and the first
// 8 hex characters of its id. Name-based slugs never take this shape.
var tempSlugPattern = regexp.MustCompile(`^talk-[0-9a-f]{8}$`)

// suffixLengths are the id characters tried, in turn, as a collision suffix.
var suffixLengths = []int{4, 6, 8, 12, 32}

// TempTalkSlug is the slug of a temporary Talk. It is set once, never changes
// on a rename and is never recorded in the slug history: a leader who may
// rename a Talk but not manage channels can learn nothing about other slugs.
func TempTalkSlug(id uuid.UUID) string {
	return "talk-" + hexOf(id)[:8]
}

func hexOf(id uuid.UUID) string { return strings.ReplaceAll(id.String(), "-", "") }

// slugCandidate is the n-th choice for base (n counts from 0): base itself,
// then base-<first 4, 6, 8 ... characters of the channel's own id>. The
// suffix comes from the channel's id, never from a count of other channels,
// so a collision does not reveal how many channels use a slug. base is
// shortened so the result stays within MaxSlugLength.
func slugCandidate(base string, id uuid.UUID, n int) string {
	if n <= 0 {
		return base
	}
	suffix := "-" + hexOf(id)[:suffixLengths[n-1]]
	return strings.Trim(truncate(base, MaxSlugLength-len(suffix)), "-") + suffix
}

// keepsSlug reports whether current is still a fitting slug for base: base
// itself or base with this channel's collision suffix. A rename that changes
// only case or punctuation keeps the slug, so it does not grow the history.
func keepsSlug(current, base string, id uuid.UUID) bool {
	for n := 0; n <= len(suffixLengths); n++ {
		if current == slugCandidate(base, id, n) {
			return true
		}
	}
	return false
}

// assignSlug gives the channel the slug for name inside tx: a free one (the
// base, else base with its own id suffix) or one of its own earlier slugs.
// Slugs of other channels, current or former, are never taken. The channel
// keeps its slug when the name still fits it; a temporary Talk keeps its
// fixed slug whatever its name.
func assignSlug(ctx context.Context, tx pgx.Tx, id uuid.UUID, name string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, slugLockKey); err != nil {
		return fmt.Errorf("lock slugs: %w", err)
	}
	var current *string
	var temp bool
	if err := tx.QueryRow(ctx, `SELECT slug, is_temp FROM channels WHERE id = $1`, id).Scan(&current, &temp); err != nil {
		return fmt.Errorf("read slug: %w", err)
	}
	if temp {
		return nil
	}
	base := Slugify(name)
	if tempSlugPattern.MatchString(base) {
		base += "-x"
	}
	if current != nil && keepsSlug(*current, base, id) {
		return nil
	}
	slug, err := freeSlug(ctx, tx, id, base)
	if err != nil {
		return err
	}
	return switchSlug(ctx, tx, id, current, slug)
}

// switchSlug makes slug the channel's current one. The slug it leaves becomes
// a former slug; only the newest MaxFormerSlugs are kept.
func switchSlug(ctx context.Context, tx pgx.Tx, id uuid.UUID, old *string, slug string) error {
	if _, err := tx.Exec(ctx, `UPDATE channels SET slug = $2 WHERE id = $1`, id, slug); err != nil {
		return fmt.Errorf("set slug: %w", err)
	}
	if old != nil {
		if _, err := tx.Exec(ctx, `UPDATE channel_slugs SET used_at = clock_timestamp() WHERE slug = $1`, *old); err != nil {
			return fmt.Errorf("retire slug: %w", err)
		}
	}
	_, err := tx.Exec(ctx, `
		DELETE FROM channel_slugs WHERE channel_id = $1 AND slug <> $2 AND slug NOT IN (
			SELECT slug FROM channel_slugs WHERE channel_id = $1 AND slug <> $2
			ORDER BY used_at DESC, slug DESC LIMIT $3)`, id, slug, MaxFormerSlugs)
	if err != nil {
		return fmt.Errorf("trim slug history: %w", err)
	}
	return nil
}

// freeSlug picks the first candidate nobody else holds or held, and records
// it in the channel's history.
func freeSlug(ctx context.Context, tx pgx.Tx, id uuid.UUID, base string) (string, error) {
	for n := 0; n <= len(suffixLengths); n++ {
		cand := slugCandidate(base, id, n)
		var owner uuid.UUID
		var taken bool
		// channels.slug is checked too: a temporary Talk's fixed slug has no history row.
		err := tx.QueryRow(ctx, `
			SELECT COALESCE((SELECT channel_id FROM channel_slugs WHERE slug = $1), '00000000-0000-0000-0000-000000000000'),
			       EXISTS (SELECT 1 FROM channels WHERE slug = $1 AND id <> $2)`, cand, id).Scan(&owner, &taken)
		if err != nil {
			return "", fmt.Errorf("check slug: %w", err)
		}
		if owner == id {
			return cand, nil
		}
		if owner != uuid.Nil || taken {
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO channel_slugs (slug, channel_id) VALUES ($1, $2)`, cand, id); err != nil {
			return "", fmt.Errorf("record slug: %w", err)
		}
		return cand, nil
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
	var slug *string
	if err := q.QueryRow(ctx, `SELECT slug FROM channels WHERE id = $1`, ch.ID).Scan(&slug); err != nil {
		return fmt.Errorf("load slug: %w", err)
	}
	ch.Slug = ""
	if slug != nil {
		ch.Slug = *slug
	}
	rows, err := q.Query(ctx, `
		SELECT slug FROM channel_slugs WHERE channel_id = $1 AND slug IS DISTINCT FROM $2 ORDER BY used_at, slug`, ch.ID, slug)
	if err != nil {
		return fmt.Errorf("load slugs: %w", err)
	}
	defer rows.Close()
	ch.PreviousSlugs = []string{}
	for rows.Next() {
		var former string
		if err := rows.Scan(&former); err != nil {
			return err
		}
		ch.PreviousSlugs = append(ch.PreviousSlugs, former)
	}
	return rows.Err()
}
