package channels

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// AutoDeleteChoices are the periods in days the older request field "days"
// takes. 0 turns auto-delete off.
var AutoDeleteChoices = []int{0, 1, 7, 30, 90}

// MaxAutoDeleteHours is the longest auto-delete period: 365 days.
const MaxAutoDeleteHours = 8760

// AutoDeleteEvery is how often the auto-delete sweeper runs. The runs fall on
// whole multiples of it, so the next run is known without asking the sweeper.
const AutoDeleteEvery = 10 * time.Minute

// NextAutoDeleteRun is when the sweeper runs next after now.
func NextAutoDeleteRun(now time.Time) time.Time {
	return now.Truncate(AutoDeleteEvery).Add(AutoDeleteEvery)
}

// AutoDeleteSetting is what a channel is set to. A nil Hours follows the
// community default; 0 is off for this channel.
type AutoDeleteSetting struct {
	Hours *int
	Scope string
}

// AutoDeleteRequest is the body of PUT /api/admin/channels/{id}/auto-delete
// and of its preview.
type AutoDeleteRequest struct {
	// Days is the older form: 1, 7, 30 or 90; 0 turns auto-delete off. Hours
	// wins when both are given.
	Days *int `json:"days,omitempty" enums:"0,1,7,30,90" binding:"optional"`
	// Hours is the period in hours, 1 to 8760 (365 days); 0 turns it off for
	// this channel, also when the community has a default.
	Hours *int `json:"hours" minimum:"0" maximum:"8760" binding:"optional" extensions:"x-nullable"`
	// Scope is what goes: "all" (the default) the messages with their files,
	// "attachments" only the files.
	Scope string `json:"scope" enums:"all,attachments" binding:"optional"`
	// UseDefault makes the channel follow the community default; days and
	// hours are then ignored.
	UseDefault bool `json:"use_default" binding:"optional"`
}

// Setting checks the request and turns it into a setting.
func (req AutoDeleteRequest) Setting() (AutoDeleteSetting, error) {
	scope, err := ValidAutoDeleteScope(req.Scope)
	if err != nil {
		return AutoDeleteSetting{}, err
	}
	switch {
	case req.UseDefault:
		return AutoDeleteSetting{Scope: core.AutoDeleteScopeAll}, nil
	case req.Hours != nil:
		if *req.Hours < 0 || *req.Hours > MaxAutoDeleteHours {
			return AutoDeleteSetting{}, httpx.ErrInvalidInput("hours must be 0 (off) to 8760")
		}
		h := *req.Hours
		return AutoDeleteSetting{Hours: &h, Scope: scope}, nil
	case req.Days != nil:
		if !slices.Contains(AutoDeleteChoices, *req.Days) {
			return AutoDeleteSetting{}, httpx.ErrInvalidInput("days must be 0 (off), 1, 7, 30 or 90")
		}
		h := *req.Days * 24
		return AutoDeleteSetting{Hours: &h, Scope: scope}, nil
	}
	return AutoDeleteSetting{}, httpx.ErrInvalidInput("give hours, days or use_default")
}

// ValidAutoDeleteScope checks a scope; empty means "all".
func ValidAutoDeleteScope(scope string) (string, error) {
	switch scope {
	case "", core.AutoDeleteScopeAll:
		return core.AutoDeleteScopeAll, nil
	case core.AutoDeleteScopeAttachments:
		return scope, nil
	}
	return "", httpx.ErrInvalidInput("scope must be all or attachments")
}

// SetAutoDelete changes how long a channel keeps its messages or files.
// Conversations and their call Talks never take part (not found). It returns
// the channel as it is now.
func SetAutoDelete(ctx context.Context, p *db.Pool, id uuid.UUID, s AutoDeleteSetting) (*core.Channel, error) {
	tag, err := p.Exec(ctx, `
		UPDATE channels SET auto_delete_hours = $2, auto_delete_scope = $3
		WHERE id = $1 AND NOT `+core.PrivateChannelSQL("channels"), id, s.Hours, s.Scope)
	if err != nil {
		return nil, fmt.Errorf("set auto-delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, core.ErrChannelNotFound
	}
	return LoadChannelRow(ctx, p, id)
}

// LoadChannelRow reads one community channel with its effective auto-delete,
// talk fields and slugs.
func LoadChannelRow(ctx context.Context, p *db.Pool, id uuid.UUID) (*core.Channel, error) {
	var ch core.Channel
	err := p.QueryRow(ctx, `
		SELECT id, number, category_id, name, type, topic, sort_order, created_at, user_limit, slow_mode_seconds, `+core.AutoDeleteSQL("channels")+`
		FROM channels WHERE id = $1 AND type <> 'dm'`, id).
		Scan(&ch.ID, &ch.Number, &ch.CategoryID, &ch.Name, &ch.Type, &ch.Topic, &ch.SortOrder, &ch.CreatedAt, &ch.UserLimit, &ch.SlowModeSeconds,
			&ch.AutoDeleteHours, &ch.AutoDeleteScope, &ch.AutoDeleteFollowsDefault)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, core.ErrChannelNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load channel: %w", err)
	}
	ch.FillAutoDeleteDays()
	if err := FillTalkFields(ctx, p, &ch); err != nil {
		return nil, err
	}
	if err := FillSlugs(ctx, p, &ch); err != nil {
		return nil, err
	}
	return &ch, nil
}
