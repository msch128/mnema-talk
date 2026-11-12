package communityevents

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/automod"
	"github.com/msch128/mnema-talk/internal/chat/talks"
	"github.com/msch128/mnema-talk/internal/httpx"
)

func validateEventText(title, desc string) error {
	if _, err := httpx.CleanText("title", title, 100, true); err != nil {
		return err
	}
	_, err := httpx.CleanText("description", desc, 2000, false)
	return err
}

func validateEventTimes(start time.Time, end *time.Time) error {
	if start.IsZero() {
		return httpx.ErrInvalidInput("start_time is required")
	}
	if end != nil && end.Before(start) {
		return httpx.ErrInvalidInput("end_time must be at or after start_time")
	}
	return nil
}

var errNotATalk = httpx.ErrInvalidInput("channel_id must be a Talk (voice) channel")

func (h *Handler) validateEventLocation(r *http.Request, chID *uuid.UUID, loc *string) error {
	hasChannel := chID != nil
	hasLoc := loc != nil && strings.TrimSpace(*loc) != ""

	if !hasChannel && !hasLoc {
		return httpx.ErrInvalidInput("either channel_id (Talk) or location (text) must be set")
	}
	if hasChannel && hasLoc {
		return httpx.ErrInvalidInput("cannot specify both channel_id and location text")
	}
	if hasChannel {
		return h.validateEventTalk(r, *chID)
	}
	_, err := httpx.CleanText("location", *loc, 255, true)
	return err
}

// validateEventTalk checks that chID is a Talk (voice) channel of the community
// that the caller may view. A conversation's call Talk is no community channel:
// an event is a community object, audited and listed for the community, and
// the call's members are the only ones who may know it exists. It is refused
// like a channel that does not exist (same status, same bytes), and a failed
// lookup refuses the request instead of guessing.
func (h *Handler) validateEventTalk(r *http.Request, chID uuid.UUID) error {
	var kind string
	var isCall bool
	err := h.DB.QueryRow(r.Context(), `SELECT type, dm_id IS NOT NULL FROM channels WHERE id = $1`, chID).Scan(&kind, &isCall)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err != nil || kind != "voice" || isCall {
		return errNotATalk
	}
	if !h.canViewChannel(r, chID) {
		return httpx.ErrForbidden("you cannot view this Talk channel")
	}
	return nil
}

func (h *Handler) validateEventPayload(r *http.Request, title, desc string, start time.Time, end *time.Time, chID *uuid.UUID, loc *string) error {
	if err := validateEventText(title, desc); err != nil {
		return err
	}
	if err := validateEventTimes(start, end); err != nil {
		return err
	}
	return h.validateEventLocation(r, chID, loc)
}

// auditable reports whether an entry about ev may be written. An event that
// names a conversation's call Talk (one made before such a location was
// refused) is not audited: the entry would carry the call's id and the event's
// title to the Owner and the Administrators outside the conversation. When
// the lookup fails nothing is written either.
func (h *Handler) auditable(r *http.Request, ev *CommunityEvent) bool {
	if h.Audit == nil || ev == nil {
		return false
	}
	if ev.ChannelID == nil {
		return true
	}
	isCall, err := talks.IsDMTalk(r.Context(), h.DB, *ev.ChannelID)
	if err != nil {
		slog.WarnContext(r.Context(), "event audit skipped, Talk lookup failed", "event_id", ev.ID, "err", err)
	}
	return err == nil && !isCall
}

func (h *Handler) auditRecord(r *http.Request, action string, ev *CommunityEvent) {
	if !h.auditable(r, ev) {
		return
	}
	h.Audit.RecordOrLog(r.Context(), eventEntry(action, ev))
}

// eventEntry is the audit entry of action on ev.
func eventEntry(action string, ev *CommunityEvent) audit.Entry {
	if ev.ChannelID != nil {
		// A Talk event names a possibly private Talk: only auditors who can
		// view that Talk read it (class event.talk_%).
		action = "event.talk_" + strings.TrimPrefix(action, "event.")
	}
	return audit.Entry{
		Action:     action,
		TargetType: "event",
		TargetID:   ev.ID.String(),
		TargetName: ev.Title,
		ChannelID:  ev.ChannelID,
		Details: map[string]any{
			"title":      ev.Title,
			"start_time": ev.StartTime.Format(time.RFC3339),
			"channel_id": ev.ChannelID,
			"location":   ev.Location,
			"status":     ev.Status,
		},
	}
}

func validStatus(s string) bool {
	switch s {
	case StatusScheduled, StatusActive, StatusCompleted, StatusCancelled:
		return true
	}
	return false
}

func trimPtr(s *string) {
	if s != nil {
		*s = strings.TrimSpace(*s)
	}
}

func (r *CreateEventRequest) normalize() {
	r.Title = strings.TrimSpace(r.Title)
	r.Description = strings.TrimSpace(r.Description)
	trimPtr(r.Location)
	if r.Location != nil && *r.Location == "" {
		r.Location = nil // an empty location is none, also next to a Talk
	}
}

func (r *UpdateEventRequest) normalize() {
	trimPtr(r.Title)
	trimPtr(r.Description)
	trimPtr(r.Location)
}

// screen applies the automod rules to text a member writes on an event (a
// comment, or the event's title, description and location) as to a message in
// the event's Talk: the same refusal, the same exempt members, roles and
// channels, and a rule's timeout. A flag rule's hits come back pending; the
// caller commits them once the text is stored. The queue names the event by
// its title and keeps no message id (the text is not a message).
func (h *Handler) screen(r *http.Request, talk *uuid.UUID, title string, edit bool, texts ...string) (*automod.Pending, error) {
	var parts []string
	for _, t := range texts {
		if strings.TrimSpace(t) != "" {
			parts = append(parts, t)
		}
	}
	if len(parts) == 0 {
		return nil, nil
	}
	p := automod.Post{UserID: auth.UserFrom(r.Context()).ID, Content: strings.Join(parts, "\n"), Edit: edit, Place: title}
	if talk != nil {
		p.ChannelID = *talk
	}
	return automod.Of(h.DB).Check(r.Context(), p)
}

// screenComment screens a new comment or, with id set, an edit of one. As on
// the message path, text the store refuses anyway (empty, too long) and an
// edit of another member's comment (403) are left to that refusal.
func (h *Handler) screenComment(r *http.Request, ev *CommunityEvent, id uuid.UUID, content string) (*automod.Pending, error) {
	text, err := httpx.CleanText("content", content, MaxCommentLen, true)
	if err != nil {
		return nil, nil
	}
	if id != uuid.Nil {
		c, err := h.Store.GetComment(r.Context(), ev.ID, id)
		if err != nil {
			return nil, err // a missing comment answers 404, as the edit would
		}
		if c.User.ID != auth.UserFrom(r.Context()).ID {
			return nil, nil
		}
	}
	return h.screen(r, ev.ChannelID, ev.Title, id != uuid.Nil, text)
}

// commitAfter records the flags of screened text once err says it was stored.
func commitAfter(r *http.Request, pending *automod.Pending, err error) error {
	if err == nil {
		pending.Commit(r.Context(), uuid.Nil)
	}
	return err
}

// screenEdit screens the fields an event edit changes; the fields it leaves
// alone are not judged again.
func (h *Handler) screenEdit(r *http.Request, m merged, req UpdateEventRequest) (*automod.Pending, error) {
	var texts []string
	for _, f := range []*string{req.Title, req.Description, req.Location} {
		if f != nil {
			texts = append(texts, *f)
		}
	}
	return h.screen(r, m.channel, m.title, true, texts...)
}
