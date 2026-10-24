package communityevents

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
)

// talkEvent is an event held in a Talk.
type talkEvent struct{ id, channel uuid.UUID }

// PermissionsChanged (perm.Notifier) tells members who can no longer view the
// Talk of an event to drop it. nil userIDs means everybody. It runs in the
// background: the permission change must not wait for it.
func (h *Handler) PermissionsChanged(userIDs []uuid.UUID) {
	if h.Events == nil || h.Perms == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := h.RetractHidden(ctx, userIDs); err != nil {
			slog.ErrorContext(ctx, "retract hidden events failed", "err", err)
		}
	}()
}

// RetractHidden sends event_delete, per event, to the members among userIDs
// (all members when nil) who could view its Talk before the change and cannot
// now. Members who never saw the event learn nothing. Only events the client
// can still list (not ended more than a day ago) are covered.
func (h *Handler) RetractHidden(ctx context.Context, userIDs []uuid.UUID) error {
	rows, err := h.DB.Query(ctx, `
		SELECT id, channel_id FROM events
		WHERE channel_id IS NOT NULL AND COALESCE(end_time, start_time) >= $1`, time.Now().Add(-listLookBehind))
	if err != nil {
		return err
	}
	var list []talkEvent
	for rows.Next() {
		var e talkEvent
		if err := rows.Scan(&e.id, &e.channel); err != nil {
			rows.Close()
			return err
		}
		list = append(list, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(list) == 0 {
		return err
	}
	prev := h.Perms.Previous()
	if prev == nil {
		return nil // nothing is known about who saw what before
	}
	snap, err := h.Perms.Snapshot(ctx)
	if err != nil {
		return err
	}
	if userIDs == nil {
		if userIDs, err = h.Store.memberIDs(ctx); err != nil {
			return err
		}
	}
	for _, e := range list {
		h.sendDelete(snap, e, userIDs, func(id uuid.UUID) bool {
			return prev.Channel(perm.Subject{ID: id}, e.channel).Has(perm.ViewChannel)
		})
	}
	return nil
}

// retractFor sends event_delete for ev to the members who could see it where
// it was (anyone, when it was not in a Talk) and cannot view its new Talk.
func (h *Handler) retractFor(ctx context.Context, ev *CommunityEvent, from *uuid.UUID) error {
	if ev.ChannelID == nil || h.Events == nil || h.Perms == nil {
		return nil
	}
	snap, err := h.Perms.Snapshot(ctx)
	if err != nil {
		return err
	}
	ids, err := h.Store.memberIDs(ctx)
	if err != nil {
		return err
	}
	h.sendDelete(snap, talkEvent{ev.ID, *ev.ChannelID}, ids, func(id uuid.UUID) bool {
		return from == nil || snap.Channel(perm.Subject{ID: id}, *from).Has(perm.ViewChannel)
	})
	return nil
}

// sendDelete tells the members who sawBefore the event and cannot view its
// Talk now to drop it. The payload is the id alone.
func (h *Handler) sendDelete(snap *perm.Snapshot, e talkEvent, userIDs []uuid.UUID, sawBefore func(uuid.UUID) bool) {
	var hidden []uuid.UUID
	for _, id := range userIDs {
		if sawBefore(id) && !snap.Channel(perm.Subject{ID: id}, e.channel).Has(perm.ViewChannel) {
			hidden = append(hidden, id)
		}
	}
	if len(hidden) > 0 {
		h.Events.SendToUsers(hidden, events.EventDelete, EventDeletePayload{ID: e.id})
	}
}

func (s *Store) memberIDs(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := s.DB.Query(ctx, `SELECT id FROM users`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
