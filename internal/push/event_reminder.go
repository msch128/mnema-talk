package push

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
)

// SendEventReminder pushes an event reminder to the member's devices,
// respecting quiet presence (DND/focus) and channel notification settings.
func (s *Service) SendEventReminder(ctx context.Context, userID uuid.UUID, eventID uuid.UUID, title string, startTime time.Time, channelID *uuid.UUID) error {
	if s == nil || s.Sender == nil {
		return nil
	}
	s.init()

	var presence, locale string
	if err := s.DB.QueryRow(ctx, `SELECT COALESCE(presence, 'offline'), COALESCE(locale, 'en') FROM users WHERE id = $1`, userID).Scan(&presence, &locale); err != nil {
		return err
	}
	if quiet(presence) {
		return nil
	}

	if channelID != nil {
		var chLvl, catLvl, srvLvl *string
		var chSetting, catSetting, srvSetting chat.NotifySetting
		var dbNow time.Time
		_ = s.DB.QueryRow(ctx, `
			SELECT r.notify_level, r.muted_until, cn.notify_level, cn.muted_until, sn.notify_level, sn.muted_until, NOW()
			FROM channels c
			LEFT JOIN channel_reads r ON r.channel_id = c.id AND r.user_id = $1
			LEFT JOIN category_notify cn ON cn.category_id = c.category_id AND cn.user_id = $1
			LEFT JOIN server_notify sn ON sn.user_id = $1
			WHERE c.id = $2`, userID, *channelID).Scan(&chLvl, &chSetting.MutedUntil, &catLvl, &catSetting.MutedUntil, &srvLvl, &srvSetting.MutedUntil, &dbNow)

		chSetting.Level = levelOf(chLvl)
		catSetting.Level = levelOf(catLvl)
		srvSetting.Level = levelOf(srvLvl)
		if chat.ResolveNotify(dbNow, chSetting, catSetting, srvSetting) == chat.NotifyMute {
			return nil
		}
	}

	targets, err := targetsFor(ctx, s.DB, []uuid.UUID{userID})
	if err != nil {
		return err
	}
	userTargets := targets[userID]
	if len(userTargets) == 0 {
		return nil
	}

	body := text(locale, "event_reminder")
	if body == "" {
		body = "Starts in 15 minutes"
	}

	p := Payload{
		Version:   1,
		Type:      "event",
		Title:     title,
		Body:      body,
		Tag:       fmt.Sprintf("event:%s", eventID),
		ChannelID: channelID,
		Timestamp: startTime.UnixMilli(),
	}
	bytes, err := p.encode()
	if err != nil {
		return err
	}

	for _, t := range userTargets {
		s.deliver(ctx, job{
			target:  t,
			payload: bytes,
			opts:    Options{TTL: time.Hour, Urgent: true, Timeout: sendTimeout},
		})
	}
	return nil
}
