package chat

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
)

// Mention keywords: @all reaches every active member, @here active connected members.
const (
	MentionAll  = "all"
	MentionHere = "here"
)

// MentionPolicy limits whom a message notifies, from the author's permissions
// in the channel. The zero value limits nothing.
type MentionPolicy struct {
	// NoEveryone turns @all and @here into plain text.
	NoEveryone bool
	// NoUsers turns @username into plain text, except for AllowUsers.
	NoUsers bool
	// AllowUsers are the only users notified when NoUsers is set.
	AllowUsers []uuid.UUID
	// CanView, when set, drops mentioned users who cannot view the channel.
	CanView func(userID uuid.UUID) bool
}

type mentionPolicyKey struct{}

// WithMentionPolicy attaches the policy that ResolveMentions (and so
// InsertMessage and EditMessage) applies.
func WithMentionPolicy(ctx context.Context, p MentionPolicy) context.Context {
	return context.WithValue(ctx, mentionPolicyKey{}, p)
}

func mentionPolicyFrom(ctx context.Context) MentionPolicy {
	p, _ := ctx.Value(mentionPolicyKey{}).(MentionPolicy)
	return p
}

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
// every active member for @all and active online users for @here. The author is
// never mentioned by their own message.
func ResolveMentions(ctx context.Context, q db.Querier, content string, authorID uuid.UUID, online []uuid.UUID) ([]uuid.UUID, error) {
	names := ParseMentions(content)
	if len(names) == 0 {
		return nil, nil
	}
	policy := mentionPolicyFrom(ctx)
	targets := splitMentionTargets(names, policy)
	ids, err := mentionIDs(ctx, q, targets, online)
	if err != nil {
		return nil, err
	}
	delete(ids, authorID)
	return allowedMentions(ids, policy, targets), nil
}

func needsMentionQuery(targets mentionTargets, online []uuid.UUID) bool {
	if targets.all || len(targets.usernames) > 0 {
		return true
	}
	if !targets.here {
		return false
	}
	return len(online) > 0
}

func mentionIDs(ctx context.Context, q db.Querier, targets mentionTargets, online []uuid.UUID) (map[uuid.UUID]bool, error) {
	ids := map[uuid.UUID]bool{}
	if !needsMentionQuery(targets, online) {
		return ids, nil
	}
	found, err := queryMentioned(ctx, q, targets, online)
	if err != nil {
		return nil, err
	}
	for _, id := range found {
		ids[id] = true
	}
	return ids, nil
}

func allowedMentions(ids map[uuid.UUID]bool, policy MentionPolicy, targets mentionTargets) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(ids))
	for id := range ids {
		if mentionSkipped(policy, targets, id) {
			continue
		}
		if !mentionVisible(policy, id) {
			continue
		}
		out = append(out, id)
	}
	return out
}

func mentionSkipped(policy MentionPolicy, targets mentionTargets, id uuid.UUID) bool {
	if !policy.NoUsers {
		return false
	}
	if targets.all || targets.here {
		return false
	}
	return !slices.Contains(policy.AllowUsers, id)
}

func mentionVisible(policy MentionPolicy, id uuid.UUID) bool {
	if policy.CanView == nil {
		return true
	}
	return policy.CanView(id)
}

// mentionTargets is what the parsed names of a message address: everyone,
// the online members, and named users.
type mentionTargets struct {
	all, here bool
	usernames []string
}

// splitMentionTargets sorts names into @all, @here and usernames; the
// policy can switch the two group mentions off.
func splitMentionTargets(names []string, policy MentionPolicy) mentionTargets {
	t := mentionTargets{usernames: make([]string, 0, len(names))}
	for _, n := range names {
		switch n {
		case MentionAll:
			t.all = !policy.NoEveryone
		case MentionHere:
			t.here = !policy.NoEveryone
		default:
			t.usernames = append(t.usernames, n)
		}
	}
	return t
}

func queryMentioned(ctx context.Context, q db.Querier, t mentionTargets, online []uuid.UUID) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, `
		SELECT id FROM users
		WHERE `+auth.ActiveSQL("")+` AND (
		    (NOT is_bot AND ($1 OR LOWER(username) = ANY($2) OR ($3 AND id = ANY($4::uuid[]))))
		    -- a bot is mentioned by name only, never by @all or @here
		    OR (LOWER(username) = ANY($2) AND EXISTS (SELECT 1 FROM bots b WHERE b.user_id = users.id AND NOT b.disabled)))`, t.all, t.usernames, t.here, online)
	if err != nil {
		return nil, fmt.Errorf("resolve mentions: %w", err)
	}
	found, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("resolve mentions: %w", err)
	}
	return found, nil
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
