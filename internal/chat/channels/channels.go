package channels

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/chat/stage"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

func populateChannels(ctx context.Context, p *db.Pool, categories []core.Category, categoryIdx map[uuid.UUID]int) ([]core.Channel, error) {
	chanRows, err := p.Query(ctx, `
		SELECT id, number, category_id, name, type, topic, sort_order, created_at, user_limit, slow_mode_seconds, `+core.AutoDeleteSQL("channels")+`,
		       is_lobby, is_temp, leader_id, is_stage, COALESCE(slug, ''),
		       COALESCE((SELECT array_agg(s.slug ORDER BY s.used_at, s.slug) FROM channel_slugs s
		                 WHERE s.channel_id = channels.id AND s.slug IS DISTINCT FROM channels.slug), '{}')
		FROM channels WHERE type <> 'dm' ORDER BY sort_order, created_at`)
	if err != nil {
		return nil, fmt.Errorf("query channels: %w", err)
	}
	defer chanRows.Close()

	uncategorized := make([]core.Channel, 0)
	for chanRows.Next() {
		var ch core.Channel
		if err := chanRows.Scan(&ch.ID, &ch.Number, &ch.CategoryID, &ch.Name, &ch.Type, &ch.Topic, &ch.SortOrder, &ch.CreatedAt, &ch.UserLimit, &ch.SlowModeSeconds, &ch.AutoDeleteHours, &ch.AutoDeleteScope, &ch.AutoDeleteFollowsDefault, &ch.IsLobby, &ch.IsTemp, &ch.LeaderID, &ch.IsStage, &ch.Slug, &ch.PreviousSlugs); err != nil {
			return nil, err
		}
		ch.FillAutoDeleteDays()
		if ch.CategoryID != nil {
			if idx, ok := categoryIdx[*ch.CategoryID]; ok {
				categories[idx].Channels = append(categories[idx].Channels, ch)
				continue
			}
		}
		uncategorized = append(uncategorized, ch)
	}
	return uncategorized, chanRows.Err()
}

// GetServerHierarchy returns all categories with their nested channels, plus
// uncategorized channels.
func GetServerHierarchy(ctx context.Context, p *db.Pool) ([]core.Category, []core.Channel, error) {
	categories, categoryIdx, err := loadCategories(ctx, p)
	if err != nil {
		return nil, nil, err
	}
	uncategorized, err := populateChannels(ctx, p, categories, categoryIdx)
	if err != nil {
		return nil, nil, err
	}
	return categories, uncategorized, nil
}

func cleanChannelInputs(name, topic string, chType core.ChannelType) (string, string, error) {
	if chType != core.ChannelTypeText && chType != core.ChannelTypeVoice && chType != core.ChannelTypeForum {
		return "", "", httpx.ErrInvalidInput("type must be 'text', 'voice' or 'forum'")
	}
	cleanName, err := httpx.CleanText("name", name, core.MaxChannelNameLen, true)
	if err != nil {
		return "", "", err
	}
	cleanTopic, err := httpx.CleanText("topic", topic, core.MaxTopicLen, false)
	if err != nil {
		return "", "", err
	}
	return cleanName, cleanTopic, nil
}

// errNoCategory is what creating a channel in a category answers when the
// category does not exist and when the caller cannot see it: one answer, so a
// hidden category cannot be told from a missing one.
var errNoCategory = httpx.ErrInvalidInput("category not found")

// lockCategory requires the category to exist and to be one the caller sees
// (canSee; nil sees all), and holds it until the end of the transaction, so it
// cannot be deleted before the channel is in it. Existence and sight are
// decided at this one place, after the same work, so a hidden category cannot
// be told from a missing one by the answer or by how long it takes.
func lockCategory(ctx context.Context, tx pgx.Tx, canSee func(uuid.UUID) bool, categoryID *uuid.UUID) error {
	if categoryID == nil {
		return nil
	}
	var one int
	err := tx.QueryRow(ctx, `SELECT 1 FROM categories WHERE id = $1 FOR SHARE`, *categoryID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && canSee != nil && !canSee(*categoryID)) {
		return errNoCategory
	}
	return err
}

// CreateChannelWithStage adds a channel to categoryID (or to no category) and,
// with asStage, makes it a Stage in the same transaction, so a request that
// cannot be a Stage (a text channel) creates nothing.
func CreateChannelWithStage(ctx context.Context, p *db.Pool, categoryID *uuid.UUID, name string, chType core.ChannelType, topic string, sortOrder int, asStage bool) (*core.Channel, error) {
	return CreateChannelSeenBy(ctx, p, nil, categoryID, name, chType, topic, sortOrder, asStage)
}

// CreateChannelSeenBy is CreateChannelWithStage for a caller who sees the
// categories canSee names (nil: all): any other category answers as a missing one.
func CreateChannelSeenBy(ctx context.Context, p *db.Pool, canSee func(uuid.UUID) bool, categoryID *uuid.UUID, name string, chType core.ChannelType, topic string, sortOrder int, asStage bool) (*core.Channel, error) {
	name, topic, err := cleanChannelInputs(name, topic, chType)
	if err != nil {
		return nil, err
	}
	if err := CheckSortOrder(sortOrder); err != nil {
		return nil, err
	}
	var ch core.Channel
	err = pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		if err := lockCategory(ctx, tx, canSee, categoryID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO channels (category_id, name, type, topic, sort_order) VALUES ($1, $2, $3, $4, $5)
			RETURNING id, number, category_id, name, type, topic, sort_order, created_at, user_limit, slow_mode_seconds`,
			categoryID, name, chType, topic, sortOrder).
			Scan(&ch.ID, &ch.Number, &ch.CategoryID, &ch.Name, &ch.Type, &ch.Topic, &ch.SortOrder, &ch.CreatedAt, &ch.UserLimit, &ch.SlowModeSeconds); err != nil {
			return err
		}
		if asStage {
			if err := stage.SetStage(ctx, tx, ch.ID, true); err != nil {
				return err
			}
			ch.IsStage = true
		}
		return assignAndFillSlugs(ctx, tx, &ch)
	})
	var refusal *httpx.APIError
	if errors.As(err, &refusal) {
		return nil, refusal
	}
	if err != nil {
		return nil, fmt.Errorf("create channel: %w", err)
	}
	return &ch, nil
}

// assignAndFillSlugs gives ch the slug for its name and loads it into ch.
func assignAndFillSlugs(ctx context.Context, tx pgx.Tx, ch *core.Channel) error {
	if err := assignSlug(ctx, tx, ch.ID, ch.Name); err != nil {
		return err
	}
	return FillSlugs(ctx, tx, ch)
}

// DeleteChannelsTx deletes the channels and, by cascade, their messages inside
// tx and returns the storage keys of the media attached to those messages, of
// the covers of their events and of the avatars of the incoming webhooks' bot
// accounts. The caller locks the channels first and removes the objects after
// the commit.
func DeleteChannelsTx(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) ([]string, error) {
	keys, err := messages.ChannelMediaKeys(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	covers, err := messages.DeleteEventCovers(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	keys = append(keys, covers...)
	// The webhook rows go with the channels by cascade, which leaves their
	// hidden bot accounts behind: nothing would ever reference them again.
	rows, err := tx.Query(ctx, `SELECT bot_user_id FROM webhooks WHERE channel_id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("list webhook bots: %w", err)
	}
	botIDs, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("list webhook bots: %w", err)
	}
	// The bot accounts' rows are taken before the cascade locks the messages:
	// a delete of a webhook message takes its author's row before the
	// message's (messages.lockMessageForDelete), and the other order here, the
	// messages and then the account, would be a lock cycle with it.
	if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = ANY($1) FOR UPDATE`, botIDs); err != nil {
		return nil, fmt.Errorf("lock webhook bots: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM channels WHERE id = ANY($1)`, ids); err != nil {
		return nil, fmt.Errorf("delete channel: %w", err)
	}
	avatars, err := deleteOrphanBotAccounts(ctx, tx, botIDs)
	return append(keys, avatars...), err
}

// ServeDeleteChannel removes a channel and, by cascade, its messages. It returns
// the storage keys of the media attached to those messages.
func DeleteChannel(ctx context.Context, p *db.Pool, channelID uuid.UUID) ([]string, error) {
	var keys []string
	// The cascade removes every message of the channel in one statement.
	ctx = db.WithStatementTimeout(ctx, db.BulkStatementTimeout)
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		// Locking the channel first blocks concurrent posts and uploads into
		// it (their foreign keys need a share lock on it) until we are done,
		// so every attachment the cascade removes is in keys.
		var one int
		err := tx.QueryRow(ctx, `SELECT 1 FROM channels WHERE id = $1 AND type <> 'dm' FOR UPDATE`, channelID).Scan(&one)
		if errors.Is(err, pgx.ErrNoRows) {
			return core.ErrChannelNotFound
		}
		if err != nil {
			return fmt.Errorf("lock channel: %w", err)
		}
		k, derr := DeleteChannelsTx(ctx, tx, []uuid.UUID{channelID})
		keys = k
		return derr
	})
	if err != nil {
		return nil, err
	}
	return keys, nil
}

// MaxUserLimit is the highest member limit a voice channel can have.
const MaxUserLimit = 999

// EditChannel renames a channel, changes its topic, its member limit (voice
// channels only; 0 removes it), its slow mode (0 turns it off) and/or the lobby
// and Stage marks; nil keeps a field. The request is validated first and
// applied in one transaction, so a request that fails in any part changes
// nothing.
func EditChannel(ctx context.Context, p *db.Pool, id uuid.UUID, name, topic *string, userLimit, slowMode *int, lobby, stage *bool) (*core.Channel, error) {
	name, topic, err := ValidateChannelEdit(name, topic, userLimit, slowMode)
	if err != nil {
		return nil, err
	}
	var ch core.Channel
	err = pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		return applyChannelEdit(ctx, tx, id, &ch, name, topic, userLimit, slowMode, lobby, stage)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, channelEditMissed(ctx, p, id, userLimit)
	}
	var apiErr *httpx.APIError
	if errors.As(err, &apiErr) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("update channel: %w", err)
	}
	return &ch, nil
}

// applyChannelEdit writes the lobby and Stage marks and the validated fields
// of one channel edit into ch.
func applyChannelEdit(ctx context.Context, tx pgx.Tx, id uuid.UUID, ch *core.Channel, name, topic *string, userLimit, slowMode *int, lobby, isStage *bool) error {
	if lobby != nil {
		if err := SetLobby(ctx, tx, id, *lobby); err != nil {
			return err
		}
	}
	if isStage != nil {
		if err := stage.SetStage(ctx, tx, id, *isStage); err != nil {
			return err
		}
	}
	if err := tx.QueryRow(ctx, `
		UPDATE channels SET name = COALESCE($2, name), topic = COALESCE($3, topic),
			user_limit = COALESCE($4, user_limit), slow_mode_seconds = COALESCE($5, slow_mode_seconds)
		WHERE id = $1 AND type <> 'dm' AND ($4::int IS NULL OR $4 = 0 OR type = 'voice')
		RETURNING id, number, category_id, name, type, topic, sort_order, created_at, user_limit, slow_mode_seconds, `+core.AutoDeleteSQL("channels"), id, name, topic, userLimit, slowMode).
		Scan(&ch.ID, &ch.Number, &ch.CategoryID, &ch.Name, &ch.Type, &ch.Topic, &ch.SortOrder, &ch.CreatedAt, &ch.UserLimit, &ch.SlowModeSeconds, &ch.AutoDeleteHours, &ch.AutoDeleteScope, &ch.AutoDeleteFollowsDefault); err != nil {
		return err
	}
	ch.FillAutoDeleteDays()
	return assignAndFillSlugs(ctx, tx, ch)
}

// channelEditMissed explains an edit that matched no row: a user limit on a
// channel that is not a voice one, or no such channel.
func channelEditMissed(ctx context.Context, p *db.Pool, id uuid.UUID, userLimit *int) error {
	if userLimit != nil && *userLimit > 0 {
		if _, err := core.LoadChannel(ctx, p, id); err == nil {
			return httpx.ErrInvalidInput("user_limit applies to voice channels only")
		}
	}
	return core.ErrChannelNotFound
}

func ValidateChannelLimits(userLimit, slowMode *int) error {
	if slowMode != nil {
		if *slowMode < 0 || *slowMode > core.MaxSlowModeSeconds {
			return httpx.ErrInvalidInput(fmt.Sprintf("slow_mode_seconds must be between 0 (off) and %d", core.MaxSlowModeSeconds))
		}
	}
	if userLimit != nil {
		if *userLimit < 0 || *userLimit > MaxUserLimit {
			return httpx.ErrInvalidInput(fmt.Sprintf("user_limit must be between 0 (no limit) and %d", MaxUserLimit))
		}
	}
	return nil
}

func validateOptionalName(name *string) (*string, error) {
	if name == nil {
		return nil, nil
	}
	n, err := httpx.CleanText("name", *name, core.MaxChannelNameLen, true)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func validateOptionalTopic(topic *string) (*string, error) {
	if topic == nil {
		return nil, nil
	}
	t, err := httpx.CleanText("topic", *topic, core.MaxTopicLen, false)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ValidateChannelEdit checks the fields of a channel update and returns the
// cleaned name and topic (nil stays nil).
func ValidateChannelEdit(name, topic *string, userLimit, slowMode *int) (*string, *string, error) {
	if err := ValidateChannelLimits(userLimit, slowMode); err != nil {
		return nil, nil, err
	}
	validName, err := validateOptionalName(name)
	if err != nil {
		return nil, nil, err
	}
	validTopic, err := validateOptionalTopic(topic)
	if err != nil {
		return nil, nil, err
	}
	return validName, validTopic, nil
}
