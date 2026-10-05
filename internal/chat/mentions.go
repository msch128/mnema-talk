package chat

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
)

// Mention keywords: @all reaches every member, @here everyone connected.
const (
	MentionAll  = "all"
	MentionHere = "here"
)

// OnlineSource reports who has an open connection right now (the WebSocket hub).
type OnlineSource interface {
	OnlineUserIDs() []uuid.UUID
}

// mentionToken finds "@name" at the start of the text or after a character
// that cannot be part of a username (see auth.ValidateUsername).
var mentionToken = regexp.MustCompile(`(?:^|[^A-Za-z0-9_.-])@([A-Za-z0-9_.-]{3,64})`)

// ParseMentions returns the lower-cased names mentioned in content, each once,
// including the keywords "all" and "here". A trailing '.' or '-' (end of a
// sentence) is not part of the name.
func ParseMentions(content string) []string {
	seen := map[string]bool{}
	var names []string
	for _, m := range mentionToken.FindAllStringSubmatch(content, -1) {
		name := strings.ToLower(strings.TrimRight(m[1], ".-"))
		if len(name) < 3 || len(name) > 32 || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

// ResolveMentions turns the mentions in content into user IDs: named users,
// every active member for @all and the online users for @here. The author is
// never mentioned by their own message.
func ResolveMentions(ctx context.Context, q db.Querier, content string, authorID uuid.UUID, online []uuid.UUID) ([]uuid.UUID, error) {
	names := ParseMentions(content)
	if len(names) == 0 {
		return nil, nil
	}
	var all, here bool
	usernames := make([]string, 0, len(names))
	for _, n := range names {
		switch n {
		case MentionAll:
			all = true
		case MentionHere:
			here = true
		default:
			usernames = append(usernames, n)
		}
	}

	ids := map[uuid.UUID]bool{}
	if here {
		for _, id := range online {
			ids[id] = true
		}
	}
	if all || len(usernames) > 0 {
		rows, err := q.Query(ctx, `
			SELECT id FROM users
			WHERE disabled_at IS NULL AND ($1 OR LOWER(username) = ANY($2))`, all, usernames)
		if err != nil {
			return nil, fmt.Errorf("resolve mentions: %w", err)
		}
		found, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return nil, fmt.Errorf("resolve mentions: %w", err)
		}
		for _, id := range found {
			ids[id] = true
		}
	}
	delete(ids, authorID)
	out := make([]uuid.UUID, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	return out, nil
}

// SaveMentions replaces the stored mentions of messageID with userIDs.
func SaveMentions(ctx context.Context, q db.Querier, messageID uuid.UUID, userIDs []uuid.UUID) error {
	if _, err := q.Exec(ctx, `DELETE FROM message_mentions WHERE message_id = $1`, messageID); err != nil {
		return fmt.Errorf("clear mentions: %w", err)
	}
	if len(userIDs) == 0 {
		return nil
	}
	_, err := q.Exec(ctx, `
		INSERT INTO message_mentions (message_id, user_id)
		SELECT $1, u FROM unnest($2::uuid[]) AS u
		ON CONFLICT DO NOTHING`, messageID, userIDs)
	if err != nil {
		return fmt.Errorf("save mentions: %w", err)
	}
	return nil
}

// RecordMentions resolves and stores the mentions of a new or edited message.
func RecordMentions(ctx context.Context, q db.Querier, src OnlineSource, messageID, authorID uuid.UUID, content string) error {
	var online []uuid.UUID
	if src != nil {
		online = src.OnlineUserIDs()
	}
	ids, err := ResolveMentions(ctx, q, content, authorID, online)
	if err != nil {
		return err
	}
	return SaveMentions(ctx, q, messageID, ids)
}
