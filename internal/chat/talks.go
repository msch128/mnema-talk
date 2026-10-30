package chat

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// Create-a-Talk (doc/talks.md). A lobby is a Talk that nobody sits in: joining
// it opens a temporary Talk in the same category, with the lobby's permission
// overwrites and member limit, and moves the member there. This file is the
// database side; talks_manager.go keeps the live state (leader, grace).

const (
	// MaxTempTalksPerLobby caps the temporary Talks one lobby has open.
	MaxTempTalksPerLobby = 50
)

var (
	errNotLobby      = httpx.ErrConflict("this Talk is not a lobby")
	errTooManyTalks  = httpx.ErrConflict("too many open Talks for this lobby")
	errCannotConnect = httpx.ErrForbidden("you may not join Talks in this community")
	errNotTempTalk   = httpx.ErrNotFound("Talk not found")
	errNotTalkLeader = httpx.ErrForbidden("only the Talk leader can change this Talk")
)

// tempTalkNumberDigits is the room a temporary Talk's number may take in the
// name, so the base is cut the same way for every number. Numbers run across
// the whole category, so lobbies sharing a long prefix can pass the per-lobby
// cap; three digits cover a category far beyond that.
const tempTalkNumberDigits = 3

// tempTalkBase is the lobby name as the stem of its temporary Talks' names,
// cut on a rune boundary so "<base> <n>" fits MaxChannelNameLen.
func tempTalkBase(lobbyName string) string {
	room := MaxChannelNameLen - 1 - tempTalkNumberDigits
	if r := []rune(strings.TrimSpace(lobbyName)); len(r) > room {
		return strings.TrimSpace(string(r[:room]))
	}
	return strings.TrimSpace(lobbyName)
}

// lowestFreeTalkNumber is the smallest n ≥ 1 for which no name is "<base> n".
func lowestFreeTalkNumber(base string, names []string) int {
	re := regexp.MustCompile(`^` + regexp.QuoteMeta(base) + ` ([0-9]+)$`)
	used := map[int]bool{}
	for _, name := range names {
		if m := re.FindStringSubmatch(name); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil {
				used[n] = true
			}
		}
	}
	n := 1
	for used[n] {
		n++
	}
	return n
}

// SetLobby marks a voice channel as a lobby or clears the mark. Temporary
// Talks cannot be lobbies.
func SetLobby(ctx context.Context, p db.Querier, id uuid.UUID, lobby bool) error {
	tag, err := p.Exec(ctx, `UPDATE channels SET is_lobby = $2 WHERE id = $1 AND type = 'voice' AND NOT is_temp AND ($2 = FALSE OR NOT is_stage)`, id, lobby)
	if err != nil {
		return fmt.Errorf("set lobby: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if _, lerr := LoadChannel(ctx, p, id); lerr == nil {
			return httpx.ErrInvalidInput("only a permanent Talk that is no Stage can be a lobby")
		}
		return errChannelNotFound
	}
	return nil
}

// FillTalkFields loads the lobby/temporary-Talk fields of ch, which the
// create and update statements do not return.
func FillTalkFields(ctx context.Context, p *db.Pool, ch *Channel) error {
	err := p.QueryRow(ctx, `SELECT is_lobby, is_temp, leader_id, is_stage FROM channels WHERE id = $1`, ch.ID).Scan(&ch.IsLobby, &ch.IsTemp, &ch.LeaderID, &ch.IsStage)
	if errors.Is(err, pgx.ErrNoRows) {
		return errChannelNotFound
	}
	if err != nil {
		return fmt.Errorf("load talk fields: %w", err)
	}
	return nil
}

// OpenTempTalk creates a temporary Talk from lobbyID with creator as its
// leader. The copy keeps the lobby's category, member limit and permission
// overwrites, so who may see and join it is exactly who may see the lobby.
// It sorts right behind the lobby without moving any permanent channel.
func OpenTempTalk(ctx context.Context, p *db.Pool, lobbyID, creator uuid.UUID) (*Channel, error) {
	var ch *Channel
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		if err := lockChannelLayout(ctx, tx); err != nil {
			return err
		}
		lobby, err := lockSourceChannel(ctx, tx, lobbyID)
		if err != nil {
			return err
		}
		if err := checkLobby(ctx, tx, lobby); err != nil {
			return err
		}
		lobby.Name, err = nextTalkName(ctx, tx, lobby.Name, lobby.CategoryID)
		if err != nil {
			return err
		}
		if ch, err = insertChannelCopy(ctx, tx, lobby, lobby.SortOrder, true); err != nil {
			return err
		}
		return markTemp(ctx, tx, ch, lobbyID, creator)
	})
	if err != nil {
		return nil, err
	}
	return ch, nil
}

// OpenDMTalk creates a temporary Talk that belongs to the conversation dm, with
// creator as its leader. It has no lobby and no overwrites: the permission
// snapshot lets only the conversation's members see and join it (dm_id), so a
// community needs no setup for calls in conversations. It is uncategorized and
// goes with the conversation.
func OpenDMTalk(ctx context.Context, p *db.Pool, dm, creator uuid.UUID) (*Channel, error) {
	var ch *Channel
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		if err := lockChannelLayout(ctx, tx); err != nil {
			return err
		}
		name, err := nextDMTalkName(ctx, tx, dm)
		if err != nil {
			return err
		}
		id := uuid.New()
		ch = &Channel{Slug: TempTalkSlug(id), PreviousSlugs: []string{}, IsTemp: true, LeaderID: &creator}
		err = tx.QueryRow(ctx, `
			INSERT INTO channels (id, category_id, name, type, topic, sort_order, user_limit, slow_mode_seconds, dm_id, is_temp, leader_id, slug)
			VALUES ($1, NULL, $2, 'voice', '', 0, 0, 0, $3, TRUE, $4, $5)
			RETURNING id, number, category_id, name, type, topic, sort_order, created_at, user_limit, slow_mode_seconds`, id, name, dm, creator, ch.Slug).
			Scan(&ch.ID, &ch.Number, &ch.CategoryID, &ch.Name, &ch.Type, &ch.Topic, &ch.SortOrder, &ch.CreatedAt, &ch.UserLimit, &ch.SlowModeSeconds)
		if err != nil {
			return fmt.Errorf("open conversation Talk: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ch, nil
}

// IsDMTalk reports whether the channel is a Talk that belongs to a
// conversation. Nothing about such a Talk is audited: who called whom, and when,
// is as private as the conversation (not even the Owner reads it).
func IsDMTalk(ctx context.Context, q db.Querier, id uuid.UUID) bool {
	var is bool
	err := q.QueryRow(ctx, `SELECT dm_id IS NOT NULL FROM channels WHERE id = $1`, id).Scan(&is)
	return err == nil && is
}

// nextDMTalkName is "Talk n" for the lowest n the conversation's open Talks do not use.
func nextDMTalkName(ctx context.Context, tx pgx.Tx, dm uuid.UUID) (string, error) {
	var names []string
	err := tx.QueryRow(ctx, `SELECT COALESCE(array_agg(name), '{}') FROM channels WHERE dm_id = $1`, dm).Scan(&names)
	if err != nil {
		return "", fmt.Errorf("list conversation Talks: %w", err)
	}
	return fitTalkName("Talk", lowestFreeTalkNumber("Talk", names)), nil
}

// checkLobby refuses a channel that is no lobby or has too many open Talks.
func checkLobby(ctx context.Context, tx pgx.Tx, lobby *Channel) error {
	var isLobby bool
	var open int
	if err := tx.QueryRow(ctx, `
		SELECT (SELECT is_lobby FROM channels WHERE id = $1),
		       (SELECT count(*) FROM channels WHERE lobby_id = $1 AND is_temp)`, lobby.ID).Scan(&isLobby, &open); err != nil {
		return fmt.Errorf("check lobby: %w", err)
	}
	if lobby.Type != ChannelTypeVoice || !isLobby {
		return errNotLobby
	}
	if open >= MaxTempTalksPerLobby {
		return errTooManyTalks
	}
	return nil
}

// nextTalkName is "<lobby name> n" for the lowest n no temporary Talk of the
// category uses under that name. Talks opened before a rename keep their
// names; the numbering of the new name starts afresh.
func nextTalkName(ctx context.Context, tx pgx.Tx, lobbyName string, category *uuid.UUID) (string, error) {
	rows, err := tx.Query(ctx, `
		SELECT name FROM channels
		WHERE is_temp AND category_id IS NOT DISTINCT FROM $1`, category)
	if err != nil {
		return "", fmt.Errorf("list temporary Talks: %w", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return "", err
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	base := tempTalkBase(lobbyName)
	return fitTalkName(base, lowestFreeTalkNumber(base, names)), nil
}

// fitTalkName is "<base> n", with the base cut further in the unlikely case
// that n has more digits than tempTalkNumberDigits reserves.
func fitTalkName(base string, n int) string {
	num := strconv.Itoa(n)
	if r := []rune(base); len(r)+1+len(num) > MaxChannelNameLen {
		base = strings.TrimSpace(string(r[:MaxChannelNameLen-1-len(num)]))
	}
	return base + " " + num
}

func markTemp(ctx context.Context, tx pgx.Tx, ch *Channel, lobby, creator uuid.UUID) error {
	if _, err := tx.Exec(ctx, `UPDATE channels SET is_temp = TRUE, lobby_id = $2, leader_id = $3 WHERE id = $1`, ch.ID, lobby, creator); err != nil {
		return fmt.Errorf("mark temporary Talk: %w", err)
	}
	ch.IsTemp, ch.LeaderID = true, &creator
	return nil
}

// SetTalkLeader stores the leader of a temporary Talk (nil: none).
func SetTalkLeader(ctx context.Context, q db.Querier, id uuid.UUID, leader *uuid.UUID) error {
	_, err := q.Exec(ctx, `UPDATE channels SET leader_id = $2 WHERE id = $1 AND is_temp`, id, leader)
	if err != nil {
		return fmt.Errorf("set Talk leader: %w", err)
	}
	return nil
}

// TalkLeader returns the leader of a temporary Talk. ok is false when id is
// not a temporary Talk.
func TalkLeader(ctx context.Context, q db.Querier, id uuid.UUID) (leader *uuid.UUID, ok bool, err error) {
	err = q.QueryRow(ctx, `SELECT leader_id FROM channels WHERE id = $1 AND is_temp`, id).Scan(&leader)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("load Talk leader: %w", err)
	}
	return leader, true, nil
}
