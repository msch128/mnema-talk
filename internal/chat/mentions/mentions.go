package mentions

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/mention"
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
	// Roles are the roles the author may mention by name (see MentionRole);
	// any other @Role stays plain text.
	Roles []MentionRole
}

type mentionPolicyKey struct{}

// WithMentionPolicy attaches the policy that mention resolution (and so
// InsertMessage and EditMessage) applies.
func WithMentionPolicy(ctx context.Context, p MentionPolicy) context.Context {
	return context.WithValue(ctx, mentionPolicyKey{}, p)
}

// PolicyFrom is the policy WithMentionPolicy attached, or the zero value.
func PolicyFrom(ctx context.Context) MentionPolicy {
	p, _ := ctx.Value(mentionPolicyKey{}).(MentionPolicy)
	return p
}

// OnlineSource reports who has an open connection right now (the WebSocket hub).
type OnlineSource interface {
	OnlineUserIDs() []uuid.UUID
}

// ParseMentions returns the lower-cased names mentioned in content, each once,
// including the keywords "all" and "here" (see the mention package).
func ParseMentions(content string) []string { return mention.Names(content) }

// MentionedUser is a user a message notifies and why.
type MentionedUser struct {
	ID   uuid.UUID
	Kind int16
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

// mentionHit is a user the parsed names reach, before the policy: Kind is
// name when the text names them, Grouped whether @all or @here reaches them
// as well (never a bot).
type mentionHit struct {
	ID      uuid.UUID
	Kind    int16
	Grouped bool
}

func mentionIDs(ctx context.Context, q db.Querier, targets mentionTargets, online []uuid.UUID) (map[uuid.UUID]mentionHit, error) {
	ids := map[uuid.UUID]mentionHit{}
	if !needsMentionQuery(targets, online) {
		return ids, nil
	}
	found, err := queryMentioned(ctx, q, targets, online)
	if err != nil {
		return nil, err
	}
	for _, u := range found {
		ids[u.ID] = u
	}
	return ids, nil
}

func allowedMentions(ids map[uuid.UUID]mentionHit, policy MentionPolicy) []MentionedUser {
	out := make([]MentionedUser, 0, len(ids))
	for id, hit := range ids {
		if mentionSkipped(policy, id, hit.Grouped) {
			continue
		}
		if !mentionVisible(policy, id) {
			continue
		}
		out = append(out, MentionedUser{ID: id, Kind: allowedKind(policy, id, hit.Kind)})
	}
	return out
}

// allowedKind is the kind a kept mention is stored with. Under NoUsers a name
// notifies only AllowUsers; anyone else still kept is reached by @all or
// @here (see mentionSkipped), so their name is plain text and the group is
// the reason they are mentioned.
func allowedKind(policy MentionPolicy, id uuid.UUID, kind int16) int16 {
	if kind == core.MentionKindName && policy.NoUsers && !slices.Contains(policy.AllowUsers, id) {
		return core.MentionKindGroup
	}
	return kind
}

// mentionSkipped reports whether the policy turns the mention of id into plain
// text: with NoUsers only AllowUsers and those the group mentions reach
// (grouped: @all everyone, @here the online, neither a bot) are notified. A
// bot named next to @all is reached by its name alone, so it is skipped.
func mentionSkipped(policy MentionPolicy, id uuid.UUID, grouped bool) bool {
	if !policy.NoUsers || slices.Contains(policy.AllowUsers, id) {
		return false
	}
	return !grouped
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

func queryMentioned(ctx context.Context, q db.Querier, t mentionTargets, online []uuid.UUID) ([]mentionHit, error) {
	rows, err := q.Query(ctx, `
		SELECT id, CASE WHEN LOWER(username) = ANY($2) THEN 0 ELSE 1 END::smallint,
		       (NOT is_bot AND ($1 OR ($3 AND id = ANY($4::uuid[])))) FROM users
		WHERE `+auth.ActiveSQL("")+` AND (
		    (NOT is_bot AND ($1 OR LOWER(username) = ANY($2) OR ($3 AND id = ANY($4::uuid[]))))
		    -- a bot is mentioned by name only, never by @all or @here
		    OR (LOWER(username) = ANY($2) AND EXISTS (SELECT 1 FROM bots b WHERE b.user_id = users.id AND NOT b.disabled)))`, t.all, t.usernames, t.here, online)
	if err != nil {
		return nil, fmt.Errorf("resolve mentions: %w", err)
	}
	found, err := pgx.CollectRows(rows, pgx.RowToStructByPos[mentionHit])
	if err != nil {
		return nil, fmt.Errorf("resolve mentions: %w", err)
	}
	return found, nil
}

// RecordMentions resolves and stores the mentions of a new or edited message
// under the mention policy of ctx.
func RecordMentions(ctx context.Context, q db.Querier, src OnlineSource, messageID, authorID uuid.UUID, content string) error {
	return RecordMentionsUnder(ctx, q, src, PolicyFrom(ctx), messageID, authorID, content)
}

// RecordMentionsUnder is RecordMentions with the policy given by the caller
// instead of read from ctx.
func RecordMentionsUnder(ctx context.Context, q db.Querier, src OnlineSource, policy MentionPolicy, messageID, authorID uuid.UUID, content string) error {
	var online []uuid.UUID
	if src != nil {
		online = src.OnlineUserIDs()
	}
	set, err := resolveMentionSet(ctx, q, policy, content, authorID, online)
	if err != nil {
		return err
	}
	return SaveMentionSet(ctx, q, messageID, set)
}
