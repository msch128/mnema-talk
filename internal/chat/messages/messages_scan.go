package messages

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/core"
)

// The pieces scanMessages reads besides the message row itself: the quoted
// reply, the member who pinned it and the embeds.

type rawReplyFields struct {
	id          *uuid.UUID
	user        *uuid.UUID
	username    *string
	displayName *string
	avatar      *string
	content     *string
	hasMedia    bool
	isBot       *bool
	isWebhook   *bool
}

func buildReplyPreview(replyToID uuid.UUID, r rawReplyFields) *core.ReplyPreview {
	p := &core.ReplyPreview{ID: replyToID, Deleted: r.id == nil}
	if r.id != nil {
		p.UserID = *r.user
		p.Username = Deref(r.username)
		p.DisplayName = Deref(r.displayName)
		p.AvatarURL = auth.AvatarURL(r.avatar)
		p.Content = Deref(r.content)
		p.HasAttachments = r.hasMedia
		p.IsBot = r.isBot != nil && *r.isBot
		p.IsWebhook = r.isWebhook != nil && *r.isWebhook
	}
	return p
}

// rawPinner is the left-joined member who pinned a message.
type rawPinner struct {
	id                            *uuid.UUID
	username, displayName, avatar *string
}

func (p rawPinner) ref() *core.UserRef {
	if p.id == nil {
		return nil
	}
	return &core.UserRef{ID: *p.id, Username: Deref(p.username), DisplayName: Deref(p.displayName), AvatarURL: auth.AvatarURL(p.avatar)}
}

func DecodeEmbeds(raw []byte) ([]core.Embed, error) {
	if len(raw) <= 2 {
		return nil, nil
	}
	var embeds []core.Embed
	if err := json.Unmarshal(raw, &embeds); err != nil {
		return nil, fmt.Errorf("decode embeds: %w", err)
	}
	return embeds, nil
}

func Deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// rawForward is whether a message is a forward. Nothing about the original is
// read with the message: its ids would reach readers who may not see it.
type rawForward struct{ forwarded bool }

func (f rawForward) ref() *core.ForwardRef {
	if !f.forwarded {
		return nil
	}
	return &core.ForwardRef{Forwarded: true}
}
