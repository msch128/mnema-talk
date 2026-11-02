package push

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/chat"
)

// SendEventReminder pushes an event reminder to the member's devices,
// respecting quiet presence (DND/focus) and channel notification settings.
func (s *Service) SendEventReminder(ctx context.Context, userID uuid.UUID, eventID uuid.UUID, title string, startTime time.Time, channelID *uuid.UUID) error {
	if s == nil || s.Sender == nil {
		return nil
	}
	s.init()

	if quietOrMuted, err := s.eventQuiet(ctx, userID, channelID); err != nil || quietOrMuted {
		return err
	}

	opts := Options{TTL: time.Hour, Urgent: true, Timeout: sendTimeout}
	err := s.pushToMember(ctx, userID, opts, func(locale string, preview bool) Payload {
		body := text(locale, "event_reminder")
		if body == "" {
			body = "Starts in 15 minutes"
		}
		// The title of an event in a private Talk may be private too.
		shown := s.Names.Get(ctx)
		if preview {
			shown = title
		}
		return Payload{
			Version:   1,
			Type:      typeEvent,
			Title:     shown,
			Body:      body,
			Tag:       fmt.Sprintf("event:%s", eventID),
			ChannelID: channelID,
			EventID:   &eventID,
			Timestamp: startTime.UnixMilli(),
		}
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // the account was disabled meanwhile
	}
	return err
}

// talkMuted says whether the member's notification settings (Talk, category,
// server) mute the Talk now. A Talk that is gone counts as muted: there is
// nothing to remind of.
func (s *Service) talkMuted(ctx context.Context, userID, channelID uuid.UUID) (bool, error) {
	var chLvl, catLvl, srvLvl *string
	var chSetting, catSetting, srvSetting chat.NotifySetting
	var dbNow time.Time
	err := s.DB.QueryRow(ctx, `
		SELECT r.notify_level, r.muted_until, cn.notify_level, cn.muted_until, sn.notify_level, sn.muted_until, NOW()
		FROM channels c
		LEFT JOIN channel_reads r ON r.channel_id = c.id AND r.user_id = $1
		LEFT JOIN category_notify cn ON cn.category_id = c.category_id AND cn.user_id = $1
		LEFT JOIN server_notify sn ON sn.user_id = $1
		WHERE c.id = $2`, userID, channelID).Scan(&chLvl, &chSetting.MutedUntil, &catLvl, &catSetting.MutedUntil, &srvLvl, &srvSetting.MutedUntil, &dbNow)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err // fail closed: a muted Talk must not ring on a failed lookup
	}
	chSetting.Level = levelOf(chLvl)
	catSetting.Level = levelOf(catLvl)
	srvSetting.Level = levelOf(srvLvl)
	return chat.ResolveNotify(dbNow, chSetting, catSetting, srvSetting) == chat.NotifyMute, nil
}

// eventQuiet says whether an event notification is held back for the member:
// their presence is quiet (do not disturb, focus) or the Talk is muted for them.
func (s *Service) eventQuiet(ctx context.Context, userID uuid.UUID, channelID *uuid.UUID) (bool, error) {
	var presence string
	if err := s.DB.QueryRow(ctx, `SELECT COALESCE(presence, 'offline') FROM users WHERE id = $1`, userID).Scan(&presence); err != nil {
		return false, err
	}
	if quiet(presence) {
		return true, nil
	}
	if channelID != nil {
		return s.talkMuted(ctx, userID, *channelID)
	}
	return false, nil
}
