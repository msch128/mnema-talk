package messages

import (
	"context"
	"net/http"
	"slices"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/mentions"
	"github.com/msch128/mnema-talk/internal/perm"
)

// ServeSendContext checks need for the caller in ch and returns the request
// context carrying the mention policy that fits their permissions: @all and
// @here only notify when they may mention everyone, and nobody is notified
// about a message in a channel they cannot view.
func (a *API) ServeSendContext(r *http.Request, ch uuid.UUID, need perm.Set) (context.Context, error) {
	return SendContextFor(a.Guard.Perms, r.Context(), ch, need)
}

// SendContextFor is SendContext for work that runs without a request (a
// scheduled message): ctx carries the author (auth.WithUser) and the
// permission state is read now, so a later change of rights applies.
func SendContextFor(perms *perm.Service, ctx context.Context, ch uuid.UUID, need perm.Set) (context.Context, error) {
	ctx, snap, p, err := gatedContext(perms, ctx, ch, need)
	if err != nil {
		return nil, err
	}
	return mentions.WithMentionPolicy(ctx, memberMentionPolicy(snap, ch, p)), nil
}

// memberMentionPolicy is the mention policy of a member's message: @all and
// @here only notify when they may mention everyone (p), roles when they may
// mention them, and nobody is notified about a message in a channel they
// cannot view.
func memberMentionPolicy(snap *perm.Snapshot, ch uuid.UUID, p perm.Set) mentions.MentionPolicy {
	policy := viewersOnly(snap, ch)
	policy.NoEveryone = !p.Has(perm.MentionEveryone)
	policy.Roles = MentionableRoles(snap, p.Has(perm.MentionEveryone))
	return policy
}

// gatedContext checks need for the author in ch and returns the context
// carrying what their permissions mean for the gate inside the insert (slow
// mode, the new member wait), without a mention policy yet. It is the gate of
// Service.Post for a member, for the entry points that still carry the rules
// in the context.
func gatedContext(perms *perm.Service, ctx context.Context, ch uuid.UUID, need perm.Set) (context.Context, *perm.Snapshot, perm.Set, error) {
	actor := MemberActor(auth.UserFrom(ctx))
	g, err := decide(ctx, perms, actor, ch, need)
	if err != nil {
		return nil, nil, 0, err
	}
	rules := g.gateRules(actor)
	ctx = WithSlowModeExempt(ctx, rules.SlowModeExempt)
	return withNewMemberWriteWait(ctx, rules.NewMemberWait), g.snap, g.perms, nil
}

// viewersOnly is the strictest mention policy: members named in the message
// are notified when they can view ch; @all, @here and roles notify nobody.
func viewersOnly(snap *perm.Snapshot, ch uuid.UUID) mentions.MentionPolicy {
	return mentions.MentionPolicy{
		NoEveryone: true,
		CanView:    func(id uuid.UUID) bool { return snap.Channel(perm.Subject{ID: id}, ch).Has(perm.ViewChannel) },
	}
}

// MentionableRoles are the roles an author may mention: those marked
// mentionable, or every custom role for an author who may mention everyone
// (the Owner and Administrators always may). @everyone and the roles the
// server manages for bots cannot be mentioned by name.
func MentionableRoles(snap *perm.Snapshot, mayAll bool) []mentions.MentionRole {
	var out []mentions.MentionRole
	for _, r := range snap.Roles() {
		if r.Everyone || r.Managed || !(r.Mentionable || mayAll) {
			continue
		}
		id := r.ID
		out = append(out, mentions.MentionRole{ID: id, Name: r.Name, Members: func() []uuid.UUID { return snap.RoleMembers(id) }})
	}
	return out
}

// ServeSendContext is Handler.sendContext for a message that another feature creates for
// the caller (a built-in slash command, a poll): the caller needs need in ch.
func SendContext(perms *perm.Service, r *http.Request, ch uuid.UUID, need perm.Set) (context.Context, error) {
	return SendContextFor(perms, r.Context(), ch, need)
}

// UploadContext is the send context of an upload (media package): the caller
// needs to view the channel, send messages and attach files.
func UploadContext(perms *perm.Service, r *http.Request, ch uuid.UUID) (context.Context, error) {
	return SendContextFor(perms, r.Context(), ch, perm.SendMessages|perm.AttachFiles)
}

// AllowedMentions is the allowed_mentions of a bot's message (Discord's
// object): Parse lists the kinds that notify ("users", "everyone", "roles"),
// Users and Roles the IDs that notify although their kind is not in Parse.
type AllowedMentions struct {
	Parse []string `json:"parse"`
	Users []string `json:"users"`
	Roles []string `json:"roles"`
}

// BotMentionPolicy is the mention policy of a bot's message: what the bot may
// do in the channel (p), narrowed by its allowed_mentions. The request can
// only narrow the permissions, never widen them. Without allowed_mentions the
// members named in the message are notified; @all, @here and roles are not.
func BotMentionPolicy(snap *perm.Snapshot, ch uuid.UUID, p perm.Set, am *AllowedMentions) mentions.MentionPolicy {
	policy := viewersOnly(snap, ch)
	if am == nil {
		return policy
	}
	policy.NoEveryone = !slices.Contains(am.Parse, "everyone") || !p.Has(perm.MentionEveryone)
	if !slices.Contains(am.Parse, "users") {
		policy.NoUsers = true
		policy.AllowUsers = parseAllowedUsers(am.Users)
	}
	// Roles follow the bot's permissions like a member's and only notify when
	// the request asked for them.
	policy.Roles = mentions.AllowedRoles(MentionableRoles(snap, p.Has(perm.MentionEveryone)), am.Parse, am.Roles)
	return policy
}

func parseAllowedUsers(rawIDs []string) []uuid.UUID {
	var ids []uuid.UUID
	for _, raw := range rawIDs {
		if id, err := uuid.Parse(raw); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// BotUploadContext is UploadContext for the bot API's upload route: the same
// checks, and the caption follows a bot's mention rules. The form is read
// after the checks, so the context starts with the policy of a message without
// allowed_mentions; the returned function gives the context of the caption
// once the form's allowed_mentions (nil when absent) is known.
func BotUploadContext(perms *perm.Service, r *http.Request, ch uuid.UUID) (context.Context, func(*AllowedMentions) context.Context, error) {
	ctx, snap, p, err := gatedContext(perms, r.Context(), ch, perm.SendMessages|perm.AttachFiles)
	if err != nil {
		return nil, nil, err
	}
	with := func(am *AllowedMentions) context.Context {
		return mentions.WithMentionPolicy(ctx, BotMentionPolicy(snap, ch, p, am))
	}
	return with(nil), with, nil
}
