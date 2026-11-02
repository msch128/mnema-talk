package communityevents

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
)

const (
	// SignupKindPromoted: the member moved up from the waitlist.
	SignupKindPromoted = "promoted"
	// SignupKindRemoved: a moderator removed the member from the roster.
	SignupKindRemoved = "removed"
	// SignupKindWaitlisted: the member's role was removed and they were moved to a full one.
	SignupKindWaitlisted = "waitlisted"

	noticePushTimeout = time.Minute
)

// signupPusher sends the push of a changed sign-up to one member.
type signupPusher interface {
	SendEventSignupNotice(ctx context.Context, userID, eventID uuid.UUID, title, kind, roleName string, startTime time.Time, channelID *uuid.UUID) error
}

func (h *Handler) signupPusher() signupPusher {
	if h.pusher != nil {
		return h.pusher
	}
	if h.Push != nil {
		return h.Push
	}
	return nil
}

// WaitNotices blocks until the pushes started so far are done (tests, shutdown).
func (h *Handler) WaitNotices() { h.notices.Wait() }

// notifyPromoted tells the members who moved up from a waitlist.
func (h *Handler) notifyPromoted(r *http.Request, ev *CommunityEvent, promoted []Promotion) {
	h.notifyMembers(r.Context(), ev, SignupKindPromoted, "", promoted)
}

// notifyMembers tells each member over WebSocket right away and by push in the
// background (push honours do-not-disturb and a muted Talk). A member who can
// no longer view the event's Talk is told nothing. roleName overrides the name
// carried by the items (they carry their own when it is empty).
func (h *Handler) notifyMembers(ctx context.Context, ev *CommunityEvent, kind, roleName string, items []Promotion) {
	if len(items) == 0 {
		return
	}
	snap := h.noticeSnapshot(ctx, ev)
	if snap == denied {
		return
	}
	pusher := h.signupPusher()
	for _, it := range items {
		if ev.ChannelID != nil && snap != nil && !snap.Channel(perm.Subject{ID: it.UserID}, *ev.ChannelID).Has(perm.ViewChannel) {
			continue
		}
		kindOf := kind
		if it.Kind != "" {
			kindOf = it.Kind
		}
		role := it.RoleName
		if role == "" {
			role = roleName
		}
		if h.Events != nil {
			h.Events.SendToUsers([]uuid.UUID{it.UserID}, events.EventSignupNotice, SignupNoticePayload{
				EventID: ev.ID, Title: ev.Title, Kind: kindOf, RoleName: role, StartTime: ev.StartTime, ChannelID: ev.ChannelID,
			})
		}
		if pusher != nil {
			h.pushNotice(ctx, pusher, ev, it.UserID, kindOf, role)
		}
	}
}

// denied marks "the permission state could not be read": nobody is told about a Talk event then.
var denied = &perm.Snapshot{}

func (h *Handler) noticeSnapshot(ctx context.Context, ev *CommunityEvent) *perm.Snapshot {
	if ev.ChannelID == nil || h.Perms == nil {
		return nil
	}
	snap, err := h.Perms.Snapshot(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "load permissions for a sign-up notice failed", "event_id", ev.ID, "err", err)
		return denied
	}
	return snap
}

func (h *Handler) pushNotice(ctx context.Context, p signupPusher, ev *CommunityEvent, userID uuid.UUID, kind, role string) {
	h.notices.Add(1)
	bg := context.WithoutCancel(ctx)
	go func() {
		defer h.notices.Done()
		bg, cancel := context.WithTimeout(bg, noticePushTimeout)
		defer cancel()
		if err := p.SendEventSignupNotice(bg, userID, ev.ID, ev.Title, kind, role, ev.StartTime, ev.ChannelID); err != nil {
			slog.DebugContext(bg, "send sign-up push failed", "user_id", userID, "err", err)
		}
	}()
}
