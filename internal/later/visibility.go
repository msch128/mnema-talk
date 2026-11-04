package later

import (
	"context"
	"fmt"
	"net/http"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/textcut"
)

// snippetRunes bounds the text of a message shown in a reminder.
const snippetRunes = 200

// MessageRef is what a reminder shows of its message. It is only sent while the
// owner may still see the message (the check runs on every read).
type MessageRef struct {
	ID        uuid.UUID  `json:"id" format:"uuid"`
	ChannelID uuid.UUID  `json:"channel_id" format:"uuid"`
	ParentID  *uuid.UUID `json:"parent_id" format:"uuid" extensions:"x-nullable"`
	// ChannelName is empty for a direct message conversation.
	ChannelName string `json:"channel_name"`
	DM          bool   `json:"dm"`
	Author      string `json:"author"`
	// Snippet is the start of the message text (up to 200 characters).
	Snippet string `json:"snippet"`
}

// loadRef reads a message with what a reminder shows of it. It returns nil
// when the message does not exist.
func (s *Service) loadRef(ctx context.Context, id uuid.UUID) (*MessageRef, error) {
	refs, err := s.loadRefs(ctx, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}
	return refs[id], nil
}

// loadRefs reads several messages in one query, keyed by id; a message that
// does not exist is missing from the result.
func (s *Service) loadRefs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*MessageRef, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT m.id, m.channel_id, m.parent_id, c.name, m.in_dm, COALESCE(m.author_name, u.display_name), m.content
		FROM messages m JOIN channels c ON c.id = m.channel_id JOIN users u ON u.id = m.user_id
		WHERE m.id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("load message: %w", err)
	}
	defer rows.Close()
	refs := make(map[uuid.UUID]*MessageRef, len(ids))
	for rows.Next() {
		var m MessageRef
		var content string
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.ParentID, &m.ChannelName, &m.DM, &m.Author, &content); err != nil {
			return nil, fmt.Errorf("load message: %w", err)
		}
		m.Snippet = cut(content, snippetRunes)
		if m.DM {
			m.ChannelName = ""
		}
		refs[m.ID] = &m
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load message: %w", err)
	}
	return refs, nil
}

func cut(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return textcut.Runes(s, limit-1) + "…"
}

// canView reports whether the member may see the channel right now. A
// conversation is visible to its members only (the snapshot knows that rule).
// It fails closed: an unreadable permission state answers an error.
func (s *Service) canView(ctx context.Context, sub perm.Subject, channel uuid.UUID) (bool, error) {
	snap, err := s.Perms.ForChannel(ctx, channel)
	if err != nil {
		return false, err
	}
	return snap.Channel(sub, channel).Has(perm.ViewChannel), nil
}

// subjectOf is who an account is for permission questions; ok is false when the
// account is gone, disabled or banned.
func (s *Service) subjectOf(ctx context.Context, user uuid.UUID) (sub perm.Subject, ok bool, err error) {
	u, ok, err := s.activeUser(ctx, user)
	if err != nil || !ok {
		return sub, false, err
	}
	return u.Subject(), true, nil
}

// activeUser loads an account that may act now; ok is false when the account is
// gone, disabled or banned. The Owner rule stays with auth.User.Subject.
func (s *Service) activeUser(ctx context.Context, user uuid.UUID) (*auth.User, bool, error) {
	u, err := auth.GetActiveUser(ctx, s.DB, user)
	if apiErr, isAPI := httpx.AsAPIError(err); isAPI && apiErr.Status == http.StatusNotFound {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("load account: %w", err)
	}
	return u, true, nil
}

// visibleRef returns the message when sub may see it now, nil otherwise.
func (s *Service) visibleRef(ctx context.Context, sub perm.Subject, id uuid.UUID) (*MessageRef, error) {
	ref, err := s.loadRef(ctx, id)
	if err != nil || ref == nil {
		return nil, err
	}
	return s.visible(ctx, sub, ref)
}

// visible is ref when sub may still see its channel, otherwise nil.
func (s *Service) visible(ctx context.Context, sub perm.Subject, ref *MessageRef) (*MessageRef, error) {
	ok, err := s.canView(ctx, sub, ref.ChannelID)
	if err != nil || !ok {
		return nil, err
	}
	return ref, nil
}
