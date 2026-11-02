package communityevents

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
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

// notifyComment tells the creator and the attendees of the event, except the
// author and those who muted it, about a new comment: over WebSocket right
// away and by push in the background. A member who cannot view the event's
// Talk is told nothing.
func (h *Handler) notifyComment(ctx context.Context, ev *CommunityEvent, c EventComment) {
	ids, err := h.Store.CommentRecipients(ctx, ev.ID, c.User.ID)
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
	for _, id := range ids {
		if ev.ChannelID != nil && snap != nil && !snap.Channel(perm.Subject{ID: id}, *ev.ChannelID).Has(perm.ViewChannel) {
			continue
		}
		if h.Events != nil {
			h.Events.SendToUsers([]uuid.UUID{id}, events.EventCommentNotice, CommentNoticePayload{
				EventID: ev.ID, Title: ev.Title, AuthorName: name, Excerpt: excerpt, ChannelID: ev.ChannelID,
			})
		}
		if pusher != nil {
			h.pushComment(ctx, pusher, ev, id, name, excerpt)
		}
	}
}

func (h *Handler) pushComment(ctx context.Context, p commentPusher, ev *CommunityEvent, userID uuid.UUID, author, excerpt string) {
	h.notices.Add(1)
	bg := context.WithoutCancel(ctx)
	go func() {
		defer h.notices.Done()
		bg, cancel := context.WithTimeout(bg, noticePushTimeout)
		defer cancel()
		if err := p.SendEventCommentNotice(bg, userID, ev.ID, ev.Title, author, excerpt, ev.StartTime, ev.ChannelID); err != nil {
			slog.DebugContext(bg, "send comment push failed", "user_id", userID, "err", err)
		}
	}()
}
