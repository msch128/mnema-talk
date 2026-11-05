package chat

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/mentions"
	"github.com/msch128/mnema-talk/internal/perm"
)

// sendContext checks need for the caller in ch and returns the request
// context carrying the mention policy that fits their permissions: @all and
// @here only notify when they may mention everyone, and nobody is notified
// about a message in a channel they cannot view.
func (h *Handler) sendContext(r *http.Request, ch uuid.UUID, need perm.Set) (context.Context, error) {
	return SendContextFor(h.Perms, r.Context(), ch, need)
}

// SendContextFor is SendContext for work that runs without a request (a
// scheduled message): ctx carries the author (auth.WithUser) and the
// permission state is read now, so a later change of rights applies.
func SendContextFor(perms *perm.Service, ctx context.Context, ch uuid.UUID, need perm.Set) (context.Context, error) {
	if perms == nil {
		return ctx, nil
	}
	snap, err := perms.ForChannel(ctx, ch)
	if err != nil {
		return nil, err
	}
	sub := auth.UserFrom(ctx).Subject()
	p := snap.Channel(sub, ch)
	if err := snap.Deny(sub, p, need); err != nil {
		return nil, err
	}
	ctx = WithSlowModeExempt(ctx, p.Has(perm.ManageMessages))
	ctx = withNewMemberWriteWait(ctx, snap.NewMemberWaits(sub.ID).Write)
	return mentions.WithMentionPolicy(ctx, MentionPolicy{
		NoEveryone: !p.Has(perm.MentionEveryone),
		CanView:    func(id uuid.UUID) bool { return snap.Channel(perm.Subject{ID: id}, ch).Has(perm.ViewChannel) },
		Roles:      MentionableRoles(snap, p.Has(perm.MentionEveryone)),
	}), nil
}

// MentionableRoles are the roles an author may mention: those marked
// mentionable, or every custom role for an author who may mention everyone
// (the Owner and Administrators always may). @everyone and the roles the
// server manages for bots cannot be mentioned by name.
func MentionableRoles(snap *perm.Snapshot, mayAll bool) []MentionRole {
	var out []MentionRole
	for _, r := range snap.Roles() {
		if r.Everyone || r.Managed || !(r.Mentionable || mayAll) {
			continue
		}
		id := r.ID
		out = append(out, MentionRole{ID: id, Name: r.Name, Members: func() []uuid.UUID { return snap.RoleMembers(id) }})
	}
	return out
}

// SendContext is Handler.sendContext for a message that another feature creates for
// the caller (a built-in slash command, a poll): the caller needs need in ch.
func SendContext(perms *perm.Service, r *http.Request, ch uuid.UUID, need perm.Set) (context.Context, error) {
	return SendContextFor(perms, r.Context(), ch, need)
}

// UploadContext is the send context of an upload (media package): the caller
// needs to view the channel, send messages and attach files.
func UploadContext(perms *perm.Service, r *http.Request, ch uuid.UUID) (context.Context, error) {
	return SendContextFor(perms, r.Context(), ch, perm.SendMessages|perm.AttachFiles)
}
