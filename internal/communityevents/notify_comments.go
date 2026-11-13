package communityevents

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/safego"
)

const commentExcerptLen = 120

// commentPusher sends the push of a new comment to one member.
type commentPusher interface {
	SendEventCommentNotice(ctx context.Context, userID, eventID uuid.UUID, title, author, excerpt string, startTime time.Time, channelID *uuid.UUID) error
}

func (h *Handler) commentPusher() commentPusher {
	if h.commentsPusher != nil {
		return h.commentsPusher
	}
	if h.Push != nil {
		return h.Push
	}
	return nil
}

func excerptOf(content string) string {
	text := strings.Join(strings.Fields(content), " ")
	if r := []rune(text); len(r) > commentExcerptLen {
		return string(r[:commentExcerptLen]) + "…"
	}
	return text
}

// notifyCommentAsync runs notifyComment off the request, so a comment under a
// popular event does not hold up the member who wrote it.
func (h *Handler) notifyCommentAsync(ctx context.Context, ev *CommunityEvent, c EventComment) {
	bg := context.WithoutCancel(ctx)
	safego.GoWaited(&h.notices, "event_comment_notice", safego.Abandon, func() {
		bg, cancel := context.WithTimeout(bg, 2*noticePushTimeout)
		defer cancel()
		h.notifyComment(bg, ev, c)
	})
}

// notifyComment tells the creator and the attendees of the event, except the
// author and those who muted it, about a new comment: over WebSocket right
// away and by push in the background. A member who cannot view the event's
// Talk is told nothing.
func (h *Handler) notifyComment(ctx context.Context, ev *CommunityEvent, c EventComment) {
	recipients, err := h.Store.CommentRecipients(ctx, ev.ID, c.User.ID)
	if err != nil {
		slog.ErrorContext(ctx, "load comment recipients failed", "event_id", ev.ID, "err", err)
		return
	}
	snap := h.noticeSnapshot(ctx, ev)
	if snap == denied {
		return
	}
	pusher := h.commentPusher()
	name := c.User.DisplayName
	if name == "" {
		name = c.User.Username
	}
	excerpt := excerptOf(c.Content)
	var pushTo []uuid.UUID
	for _, rcp := range recipients {
		if len(pushTo) >= commentNotices {
			break
		}
		id := rcp.UserID
		if ev.ChannelID != nil && snap != nil && !snap.Channel(perm.Subject{ID: id}, *ev.ChannelID).Has(perm.ViewChannel) {
			continue
		}
		pushTo = append(pushTo, id)
		if h.Events != nil && !h.toastHeld(ctx, ev, rcp) {
			h.Events.SendToUsers([]uuid.UUID{id}, CommentNoticePayload{
				EventID: ev.ID, Title: ev.Title, AuthorName: name, Excerpt: excerpt, ChannelID: ev.ChannelID,
			})
		}
	}
	if pusher != nil {
		h.pushComments(ctx, pusher, ev, pushTo, name, excerpt)
	}
}

// eventQuieter says whether a member's presence or a muted Talk holds an event notice back.
type eventQuieter interface {
	EventQuiet(ctx context.Context, userID uuid.UUID, channelID *uuid.UUID) (bool, error)
}

func (h *Handler) eventQuieter() eventQuieter {
	if h.quieter != nil {
		return h.quieter
	}
	if h.Push != nil {
		return h.Push
	}
	return nil
}

// toastHeld says whether the in-app toast stays back for a recipient, as the
// push does: do not disturb, focus and a muted Talk. A failed lookup holds it
// back (a muted Talk must not ring because the lookup failed).
func (h *Handler) toastHeld(ctx context.Context, ev *CommunityEvent, rcp CommentRecipient) bool {
	if rcp.Presence == "dnd" || rcp.Presence == "focus" {
		return true
	}
	q := h.eventQuieter()
	if ev.ChannelID == nil || q == nil {
		return false
	}
	held, err := q.EventQuiet(ctx, rcp.UserID, ev.ChannelID)
	return err != nil || held
}

// commentPushWorkers is how many pushes of one comment run at once.
const commentPushWorkers = 8

// pushComments sends the pushes of one comment with a few workers; it returns when they are done.
func (h *Handler) pushComments(ctx context.Context, p commentPusher, ev *CommunityEvent, userIDs []uuid.UUID, author, excerpt string) {
	ctx, cancel := context.WithTimeout(ctx, noticePushTimeout)
	defer cancel()
	todo := make(chan uuid.UUID)
	var wg sync.WaitGroup
	for range min(commentPushWorkers, len(userIDs)) {
		wg.Add(1)
		safego.Go("event_comment_push", safego.Crash, func() {
			defer wg.Done()
			for id := range todo {
				if err := p.SendEventCommentNotice(ctx, id, ev.ID, ev.Title, author, excerpt, ev.StartTime, ev.ChannelID); err != nil {
					slog.DebugContext(ctx, "send comment push failed", "user_id", id, "err", err)
				}
			}
		})
	}
	for _, id := range userIDs {
		if ctx.Err() != nil {
			break
		}
		todo <- id
	}
	close(todo)
	wg.Wait()
}
