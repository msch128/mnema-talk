package chat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// Direct messages are conversations of two to ten members kept in the
// channels table (type 'dm'). They reuse the message pipeline, but they are
// not channels: they have their own routes and events, appear in no channel
// list, layout or channel event, and the only access rule is membership
// (perm.Snapshot.Channel). doc/architecture.md, "Direct messages", has the
// model; the privacy rules are in SECURITY.md.

// Direct message limits.
const (
	// MaxDMMembers is the most members a conversation holds.
	MaxDMMembers = 10
	// MaxGroupDMsPerMember is how many group conversations one member may be in.
	MaxGroupDMsPerMember = 100
	// MaxNewDMsPerDay is how many conversations one member may start in 24 hours.
	MaxNewDMsPerDay = 10
	// NewDMsPerMinute is how many conversations one member may try to start per minute.
	NewDMsPerMinute = 3
	// MaxUnansweredDMMessages is how many messages the member who started a
	// conversation may send before anybody else has written in it.
	MaxUnansweredDMMessages = 2
	maxDMList               = 500
)

// The refusals of the direct message rules. Whatever the reason (a block, the
// other member's privacy setting, a disabled account, a bot, a limit that is
// the other member's), the answer is the same text, so none of them can be
// probed through the API.
var (
	errDMStart      = httpx.ErrForbidden("Couldn't start this direct message.")
	errDMSend       = httpx.ErrForbidden("Couldn't send this message.")
	errDMNotFound   = httpx.ErrNotFound("direct message not found")
	errDMDayLimit   = httpx.NewAPIError(http.StatusTooManyRequests, httpx.CodeRateLimited, "You have started enough direct messages for today. Try again tomorrow.")
	errDMGroupLimit = httpx.ErrConflict(fmt.Sprintf("You are in %d group direct messages already. Leave one first.", MaxGroupDMsPerMember))
	errDMFull       = httpx.ErrConflict(fmt.Sprintf("A group direct message holds at most %d members.", MaxDMMembers))
	errDMNotGroup   = httpx.ErrInvalidInput("this direct message is with one person; start a group direct message to add more")
)

// DMMember is a member of a conversation.
type DMMember struct {
	ID          uuid.UUID `json:"id" format:"uuid"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	AvatarURL   string    `json:"avatar_url,omitempty" binding:"optional"`
}

// DirectMessage is a conversation as its member sees it.
type DirectMessage struct {
	// ID is the id messages are read and written under, in the message
	// routes (/api/channels/{id}/messages), which accept it for members only.
	ID uuid.UUID `json:"id" format:"uuid"`
	// Group is true for a conversation that can take more members; a
	// conversation with one other person is not.
	Group bool `json:"group"`
	// Members are everybody in it, the caller included.
	Members   []DMMember `json:"members"`
	CreatedAt time.Time  `json:"created_at" format:"date-time"`
	// LastMessageAt is when the latest message was written (null: none yet).
	LastMessageAt *time.Time `json:"last_message_at" format:"date-time" extensions:"x-nullable"`
	// UnreadCount counts top-level messages of others since the caller last
	// read it; MentionCount those of them that mention the caller by name or
	// reply to them.
	UnreadCount  int         `json:"unread_count"`
	MentionCount int         `json:"mention_count"`
	LastReadAt   *time.Time  `json:"last_read_at" format:"date-time" extensions:"x-nullable"`
	NotifyLevel  NotifyLevel `json:"notify_level"`
	// CanSend says whether the caller may write in it now: they hold Send
	// direct messages, the community has direct messages on, and no timeout
	// or pending rules apply. It says nothing about the other members; a
	// message that cannot be sent to them is refused when sent.
	CanSend bool `json:"can_send"`
}

const dmSelect = `
	SELECT c.id, c.dm_key IS NULL, c.created_at,
	       (SELECT MAX(m.created_at) FROM messages m WHERE m.channel_id = c.id),
	       count(um.id) FILTER (WHERE um.parent_id IS NULL),
	       count(um.id) FILTER (WHERE orig.user_id = $1 OR EXISTS (
	           SELECT 1 FROM message_mentions mm WHERE mm.message_id = um.id AND mm.user_id = $1)),
	       cr.last_read_at, COALESCE(cr.notify_level, 'inherit'), cr.muted_until, NOW()
	FROM channels c
	JOIN dm_members me ON me.channel_id = c.id AND me.user_id = $1
	LEFT JOIN channel_reads cr ON cr.channel_id = c.id AND cr.user_id = $1
	LEFT JOIN messages um ON um.channel_id = c.id AND um.user_id <> $1
	     AND um.created_at > COALESCE(cr.last_read_at, me.joined_at)
	LEFT JOIN messages orig ON orig.id = um.reply_to_id
	WHERE c.type = 'dm' AND ($2::uuid IS NULL OR c.id = $2)
	GROUP BY c.id, me.joined_at, cr.last_read_at, cr.notify_level, cr.muted_until`

// ListDMs returns the caller's conversations, the most recently active first.
// With only set, just that conversation (nil result: the caller is not in it).
func ListDMs(ctx context.Context, p *db.Pool, user uuid.UUID, only *uuid.UUID) ([]DirectMessage, error) {
	rows, err := p.Query(ctx, dmSelect+` ORDER BY 4 DESC NULLS LAST, c.created_at DESC, c.id LIMIT $3`, user, only, maxDMList)
	if err != nil {
		return nil, fmt.Errorf("list direct messages: %w", err)
	}
	dms, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (DirectMessage, error) {
		var d DirectMessage
		var level NotifySetting
		var now time.Time
		err := row.Scan(&d.ID, &d.Group, &d.CreatedAt, &d.LastMessageAt, &d.UnreadCount, &d.MentionCount,
			&d.LastReadAt, &level.Level, &level.MutedUntil, &now)
		d.NotifyLevel = ResolveNotify(now, level, NotifySetting{})
		return d, err
	})
	if err != nil {
		return nil, fmt.Errorf("list direct messages: %w", err)
	}
	return withDMMembers(ctx, p, dms)
}

// withDMMembers fills the members of each conversation, oldest member first.
func withDMMembers(ctx context.Context, p *db.Pool, dms []DirectMessage) ([]DirectMessage, error) {
	ids := make([]uuid.UUID, len(dms))
	at := make(map[uuid.UUID]int, len(dms))
	for i, d := range dms {
		ids[i], at[d.ID] = d.ID, i
		dms[i].Members = []DMMember{}
	}
	rows, err := p.Query(ctx, `
		SELECT dm.channel_id, u.id, u.username, u.display_name, u.avatar_s3_key
		FROM dm_members dm JOIN users u ON u.id = dm.user_id
		WHERE dm.channel_id = ANY($1::uuid[]) ORDER BY dm.joined_at, u.id`, ids)
	if err != nil {
		return nil, fmt.Errorf("direct message members: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ch uuid.UUID
		var m DMMember
		var avatar *string
		if err := rows.Scan(&ch, &m.ID, &m.Username, &m.DisplayName, &avatar); err != nil {
			return nil, err
		}
		m.AvatarURL = auth.AvatarURL(avatar)
		dms[at[ch]].Members = append(dms[at[ch]].Members, m)
	}
	return dms, rows.Err()
}

// dmKey names the pair of a 1:1 conversation, the same for either order.
func dmKey(a, b uuid.UUID) string {
	ids := []string{a.String(), b.String()}
	slices.Sort(ids)
	return ids[0] + ":" + ids[1]
}

// NewDM describes a conversation to start.
type NewDM struct {
	Creator uuid.UUID
	Others  []uuid.UUID
}

// eligibleSQL counts, among $1, the members $2 may start a conversation with
// or add to one: active people (not bots) who accept direct messages from
// everybody, with no block either way between them and $2 and, when $4 says
// the conversation is a group, in fewer than $3 group conversations.
var eligibleSQL = `
	SELECT COUNT(*) FROM users u
	WHERE u.id = ANY($1::uuid[]) AND ` + auth.ActiveSQL("u") + `
	  AND NOT u.is_bot AND u.dm_policy = 'everyone'
	  AND NOT EXISTS (SELECT 1 FROM user_blocks b WHERE (b.blocker_id = u.id AND b.blocked_id = $2) OR (b.blocker_id = $2 AND b.blocked_id = u.id))
	  AND (NOT $4::boolean OR (SELECT COUNT(*) FROM dm_members gm JOIN channels gc ON gc.id = gm.channel_id
	       WHERE gm.user_id = u.id AND gc.dm_key IS NULL) < $3)`

// CreateDM starts a conversation between the creator and the others, or
// returns the existing one when it is with one other person. created says
// which. Every refusal about the other members is errDMStart. The caller
// checked that the creator may start conversations at all.
func CreateDM(ctx context.Context, p *db.Pool, nd NewDM) (id uuid.UUID, created bool, err error) {
	others, err := cleanDMMembers(nd)
	if err != nil {
		return uuid.Nil, false, err
	}
	nd.Others = others
	for range 2 {
		id, created, err = createDMOnce(ctx, p, nd)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
			return id, created, err
		}
		// Somebody started the same pair a moment ago: use that one.
	}
	return id, created, err
}

// cleanDMMembers checks the list of other members: one to nine, no duplicates,
// not the creator.
func cleanDMMembers(nd NewDM) ([]uuid.UUID, error) {
	others := slices.Clone(nd.Others)
	slices.SortFunc(others, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	others = slices.Compact(others)
	switch {
	case len(others) != len(nd.Others):
		return nil, httpx.ErrInvalidInput("user_ids must not repeat a member")
	case len(others) == 0:
		return nil, httpx.ErrInvalidInput("user_ids needs at least one member")
	case len(others) > MaxDMMembers-1:
		return nil, httpx.ErrInvalidInput(fmt.Sprintf("a direct message holds at most %d members", MaxDMMembers))
	case slices.Contains(others, nd.Creator):
		return nil, httpx.ErrInvalidInput("user_ids must not contain yourself")
	}
	return others, nil
}

func createDMOnce(ctx context.Context, p *db.Pool, nd NewDM) (uuid.UUID, bool, error) {
	var id uuid.UUID
	created := false
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		// One member's starts are serialized, so the limits hold.
		if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR NO KEY UPDATE`, nd.Creator); err != nil {
			return err
		}
		group := len(nd.Others) > 1
		var key *string
		if !group {
			k := dmKey(nd.Creator, nd.Others[0])
			key = &k
			err := tx.QueryRow(ctx, `SELECT id FROM channels WHERE dm_key = $1`, k).Scan(&id)
			if err == nil {
				return nil
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		if err := checkDMStart(ctx, tx, nd, group); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO channels (number, name, type, dm_key, dm_created_by) OVERRIDING SYSTEM VALUE
			VALUES (nextval('dm_number_seq'), '', 'dm', $1, $2) RETURNING id`, key, nd.Creator).Scan(&id); err != nil {
			return err
		}
		created = true
		members := append([]uuid.UUID{nd.Creator}, nd.Others...)
		_, err := tx.Exec(ctx, `
			INSERT INTO dm_members (channel_id, user_id, added_by) SELECT $1, m, $2 FROM unnest($3::uuid[]) m`, id, nd.Creator, members)
		return err
	})
	return id, created, err
}

// checkDMStart applies the limits of the starter and the rules about the
// others, in the transaction that creates the conversation.
func checkDMStart(ctx context.Context, tx pgx.Tx, nd NewDM, group bool) error {
	var today, groups, eligible int
	err := tx.QueryRow(ctx, `
		SELECT (SELECT COUNT(*) FROM channels WHERE type = 'dm' AND dm_created_by = $2 AND created_at > NOW() - INTERVAL '24 hours'),
		       (SELECT COUNT(*) FROM dm_members gm JOIN channels gc ON gc.id = gm.channel_id WHERE gm.user_id = $2 AND gc.dm_key IS NULL),
		       (`+eligibleSQL+`)`, nd.Others, nd.Creator, MaxGroupDMsPerMember, group).Scan(&today, &groups, &eligible)
	if err != nil {
		return err
	}
	switch {
	case eligible != len(nd.Others):
		return errDMStart
	case today >= MaxNewDMsPerDay:
		return errDMDayLimit
	case group && groups >= MaxGroupDMsPerMember:
		return errDMGroupLimit
	}
	return nil
}
