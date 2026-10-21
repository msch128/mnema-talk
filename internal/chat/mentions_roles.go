package chat

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
)

// MentionRole is a role the author may mention: it is mentionable, or the
// author may mention everyone. Members is called only when the message
// actually mentions the role.
type MentionRole struct {
	ID      uuid.UUID
	Name    string
	Members func() []uuid.UUID
}

// MentionSet is the outcome of resolving a message's mentions: the users to
// notify and the roles that were mentioned (for the role pill).
type MentionSet struct {
	Users []MentionedUser
	Roles []uuid.UUID
}

// resolveMentionSet resolves names, @all, @here and role mentions of content
// under the mention policy of ctx. The author is never mentioned.
func resolveMentionSet(ctx context.Context, q db.Querier, content string, authorID uuid.UUID, online []uuid.UUID) (MentionSet, error) {
	policy := mentionPolicyFrom(ctx)
	names := ParseMentions(content)
	roles := matchRoles(content, policy.Roles)
	if len(names) == 0 && len(roles) == 0 {
		return MentionSet{}, nil
	}
	targets := splitMentionTargets(names, policy)
	ids, err := mentionIDs(ctx, q, targets, online)
	if err != nil {
		return MentionSet{}, err
	}
	delete(ids, authorID)
	set := MentionSet{Users: allowedMentions(ids, policy, targets)}
	if len(roles) == 0 {
		return set, nil
	}
	return addRoleMentions(ctx, q, set, roles, authorID, policy)
}

// addRoleMentions adds the members of the mentioned roles. A user whose name
// equals the role's name wins: that @name stays a mention of the user.
func addRoleMentions(ctx context.Context, q db.Querier, set MentionSet, roles []MentionRole, authorID uuid.UUID, policy MentionPolicy) (MentionSet, error) {
	taken, err := takenByUsers(ctx, q, roles)
	if err != nil {
		return set, err
	}
	var candidates []uuid.UUID
	have := map[uuid.UUID]bool{}
	for _, u := range set.Users {
		have[u.ID] = true
	}
	for _, r := range roles {
		if taken[strings.ToLower(r.Name)] {
			continue
		}
		set.Roles = append(set.Roles, r.ID)
		candidates = append(candidates, r.Members()...)
	}
	members, err := activeHumans(ctx, q, candidates)
	if err != nil {
		return set, err
	}
	for _, id := range members {
		if id == authorID || have[id] || !mentionVisible(policy, id) {
			continue
		}
		have[id] = true
		set.Users = append(set.Users, MentionedUser{ID: id, Kind: MentionKindRole})
	}
	return set, nil
}

// takenByUsers returns the lower-cased role names that are also the username
// of an active account.
func takenByUsers(ctx context.Context, q db.Querier, roles []MentionRole) (map[string]bool, error) {
	names := make([]string, len(roles))
	for i, r := range roles {
		names[i] = strings.ToLower(r.Name)
	}
	rows, err := q.Query(ctx, `SELECT LOWER(username) FROM users WHERE `+auth.ActiveSQL("")+` AND LOWER(username) = ANY($1)`, names)
	if err != nil {
		return nil, fmt.Errorf("resolve role mentions: %w", err)
	}
	found, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("resolve role mentions: %w", err)
	}
	taken := make(map[string]bool, len(found))
	for _, n := range found {
		taken[n] = true
	}
	return taken, nil
}

// activeHumans keeps the active, non-bot accounts among ids: a role mention
// never notifies a bot, only a direct @name does.
func activeHumans(ctx context.Context, q db.Querier, ids []uuid.UUID) ([]uuid.UUID, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := q.Query(ctx, `SELECT id FROM users WHERE `+auth.ActiveSQL("")+` AND NOT is_bot AND id = ANY($1::uuid[])`, ids)
	if err != nil {
		return nil, fmt.Errorf("resolve role mentions: %w", err)
	}
	found, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("resolve role mentions: %w", err)
	}
	return found, nil
}

type roleHit struct {
	start, end int
	role       MentionRole
}

// matchRoles finds "@Role Name" in content, case-insensitively. A mention
// ends at a character that cannot be part of a name, as for users; of two
// roles matching from the same "@" the longer name wins ("@Dev Team" is not
// also "@Dev"). Role names may contain spaces, so this scans for the known
// names instead of tokenising.
func matchRoles(content string, roles []MentionRole) []MentionRole {
	if len(roles) == 0 || !strings.Contains(content, "@") {
		return nil
	}
	var hits []roleHit
	for _, r := range roles {
		re, err := regexp.Compile(`(?i)@` + regexp.QuoteMeta(r.Name))
		if err != nil {
			continue
		}
		for _, loc := range re.FindAllStringIndex(content, -1) {
			if mentionBounded(content, loc[0], loc[1]) {
				hits = append(hits, roleHit{loc[0], loc[1], r})
			}
		}
	}
	return longestHits(hits)
}

// mentionBounded reports whether content[start:end] ("@name") starts the
// text or follows a non-name character, and is not followed by more name.
func mentionBounded(content string, start, end int) bool {
	if start > 0 && isNameChar(content[start-1]) {
		return false
	}
	if end >= len(content) {
		return true
	}
	next := content[end]
	if next == '.' && (end+1 >= len(content) || !isNameChar(content[end+1])) {
		return true
	}
	return !isNameChar(next)
}

func isNameChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '.' || c == '-'
}

func longestHits(hits []roleHit) []MentionRole {
	var out []MentionRole
	seen := map[uuid.UUID]bool{}
	for _, h := range hits {
		if seen[h.role.ID] || shadowed(h, hits) {
			continue
		}
		seen[h.role.ID] = true
		out = append(out, h.role)
	}
	return out
}

func shadowed(h roleHit, hits []roleHit) bool {
	for _, o := range hits {
		if o.start == h.start && o.end > h.end {
			return true
		}
	}
	return false
}

// saveMentionSet replaces the stored mentions and role mentions of messageID.
func saveMentionSet(ctx context.Context, q db.Querier, messageID uuid.UUID, set MentionSet) error {
	if _, err := q.Exec(ctx, `DELETE FROM message_mentions WHERE message_id = $1`, messageID); err != nil {
		return fmt.Errorf("clear mentions: %w", err)
	}
	if len(set.Users) > 0 {
		ids := make([]uuid.UUID, len(set.Users))
		kinds := make([]int16, len(set.Users))
		for i, u := range set.Users {
			ids[i], kinds[i] = u.ID, u.Kind
		}
		_, err := q.Exec(ctx, `
			INSERT INTO message_mentions (message_id, user_id, kind)
			SELECT $1, u, k FROM unnest($2::uuid[], $3::smallint[]) AS t(u, k)
			ON CONFLICT DO NOTHING`, messageID, ids, kinds)
		if err != nil {
			return fmt.Errorf("save mentions: %w", err)
		}
	}
	return saveRoleMentions(ctx, q, messageID, set.Roles)
}

func saveRoleMentions(ctx context.Context, q db.Querier, messageID uuid.UUID, roles []uuid.UUID) error {
	if _, err := q.Exec(ctx, `DELETE FROM message_role_mentions WHERE message_id = $1`, messageID); err != nil {
		return fmt.Errorf("clear role mentions: %w", err)
	}
	if len(roles) == 0 {
		return nil
	}
	_, err := q.Exec(ctx, `
		INSERT INTO message_role_mentions (message_id, role_id)
		SELECT $1, r FROM unnest($2::uuid[]) AS r ON CONFLICT DO NOTHING`, messageID, roles)
	if err != nil {
		return fmt.Errorf("save role mentions: %w", err)
	}
	return nil
}

// loadMentionRoles fills msgs[].MentionRoles.
func loadMentionRoles(ctx context.Context, p *db.Pool, msgs []Message, idx map[uuid.UUID]int, ids []uuid.UUID) error {
	rows, err := p.Query(ctx, `SELECT message_id, role_id FROM message_role_mentions WHERE message_id = ANY($1)`, ids)
	if err != nil {
		return fmt.Errorf("load role mentions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var msgID, roleID uuid.UUID
		if err := rows.Scan(&msgID, &roleID); err != nil {
			return err
		}
		msgs[idx[msgID]].MentionRoles = append(msgs[idx[msgID]].MentionRoles, roleID)
	}
	return rows.Err()
}
