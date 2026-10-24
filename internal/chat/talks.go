package chat

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"

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
	// TalkNamePrefix names a temporary Talk: "Talk 1", "Talk 2", ...
	TalkNamePrefix = "Talk "
	// MaxTempTalksPerLobby caps the temporary Talks one lobby has open.
	MaxTempTalksPerLobby = 50
)

var (
	errNotLobby       = httpx.ErrConflict("this Talk is not a lobby")
	errTooManyTalks   = httpx.ErrConflict("too many open Talks for this lobby")
	errNotTempTalk    = httpx.ErrNotFound("Talk not found")
	errNotTalkLeader  = httpx.ErrForbidden("only the Talk leader can change this Talk")
	errNoTalkLobby    = httpx.ErrConflict("this community has no Talk lobby you can use")
	tempTalkNameRegex = regexp.MustCompile(`^` + TalkNamePrefix + `([0-9]+)$`)
)

// lowestFreeTalkNumber is the smallest n ≥ 1 for which no name is "Talk n".
func lowestFreeTalkNumber(names []string) int {
	used := map[int]bool{}
	for _, name := range names {
		if m := tempTalkNameRegex.FindStringSubmatch(name); m != nil {
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
func SetLobby(ctx context.Context, p *db.Pool, id uuid.UUID, lobby bool) error {
	tag, err := p.Exec(ctx, `UPDATE channels SET is_lobby = $2 WHERE id = $1 AND type = 'voice' AND NOT is_temp`, id, lobby)
	if err != nil {
		return fmt.Errorf("set lobby: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if _, lerr := LoadChannel(ctx, p, id); lerr == nil {
			return httpx.ErrInvalidInput("only a permanent Talk can be a lobby")
		}
		return errChannelNotFound
	}
	return nil
}

// FillTalkFields loads the lobby/temporary-Talk fields of ch, which the
// create and update statements do not return.
func FillTalkFields(ctx context.Context, p *db.Pool, ch *Channel) {
	_ = p.QueryRow(ctx, `SELECT is_lobby, is_temp, leader_id FROM channels WHERE id = $1`, ch.ID).Scan(&ch.IsLobby, &ch.IsTemp, &ch.LeaderID)
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
		lobby.Name, err = nextTalkName(ctx, tx, lobby.CategoryID)
		if err != nil {
			return err
		}
		if ch, err = insertChannelCopy(ctx, tx, lobby, lobby.SortOrder); err != nil {
			return err
		}
		return markTemp(ctx, tx, ch, lobbyID, creator)
	})
	if err != nil {
		return nil, err
	}
	return ch, nil
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

// nextTalkName is "Talk n" for the lowest n no temporary Talk of the category
// uses.
func nextTalkName(ctx context.Context, tx pgx.Tx, category *uuid.UUID) (string, error) {
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
	return TalkNamePrefix + strconv.Itoa(lowestFreeTalkNumber(names)), nil
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

// FirstLobbyFor returns the first lobby (in sidebar order) that usable
// accepts, or errNoTalkLobby.
func FirstLobbyFor(ctx context.Context, p *db.Pool, usable func(id uuid.UUID) bool) (uuid.UUID, error) {
	rows, err := p.Query(ctx, `
		SELECT ch.id FROM channels ch LEFT JOIN categories c ON c.id = ch.category_id
		WHERE ch.is_lobby ORDER BY c.sort_order NULLS FIRST, ch.sort_order, ch.created_at`)
	if err != nil {
		return uuid.Nil, fmt.Errorf("list lobbies: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return uuid.Nil, err
		}
		if usable(id) {
			return id, nil
		}
	}
	if err := rows.Err(); err != nil {
		return uuid.Nil, err
	}
	return uuid.Nil, errNoTalkLobby
}
