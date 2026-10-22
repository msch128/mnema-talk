package webhook

import (
	"context"
	"slices"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/perm"
)

// mentionPolicy turns the webhook's settings and allowed_mentions into the
// policy for the message. The settings limit what a sender can ask for: with
// the defaults @username notifies members and @all and @here do not. The
// request can only narrow that, never widen it. Nobody who cannot view the
// channel is notified, whatever the settings say.
func (h *Handler) mentionPolicy(ctx context.Context, rec *record, am *AllowedMentions) (chat.MentionPolicy, error) {
	policy := chat.MentionPolicy{NoEveryone: true, NoUsers: !rec.AllowUserMentions}
	if am != nil {
		if err := narrowMentions(&policy, rec, am); err != nil {
			return policy, err
		}
	}
	// @all and @here need both the sender's request and the webhook's setting.
	policy.NoEveryone = policy.NoEveryone || !rec.AllowEveryone
	if h.Perms == nil {
		return policy, nil
	}
	snap, err := h.Perms.ForChannel(ctx, rec.ChannelID)
	if err != nil {
		return policy, err
	}
	ch := rec.ChannelID
	policy.CanView = func(id uuid.UUID) bool { return snap.Channel(perm.Subject{ID: id}, ch).Has(perm.ViewChannel) }
	if am != nil {
		// A webhook is an admin tool: its own settings, not a user's rights,
		// decide. Mentionable roles always; every custom role with
		// "allow @all/@here".
		policy.Roles = chat.AllowedRoles(chat.MentionableRoles(snap, rec.AllowEveryone), am.Parse, am.Roles)
	}
	return policy, nil
}

// narrowMentions applies the request's allowed_mentions to the policy.
func narrowMentions(policy *chat.MentionPolicy, rec *record, am *AllowedMentions) error {
	wantUsers := slices.Contains(am.Parse, "users")
	if wantUsers && len(am.Users) > 0 {
		return formError("allowed_mentions: users cannot be in parse and listed at once")
	}
	if !wantUsers && rec.AllowUserMentions {
		policy.NoUsers = true
		for _, raw := range am.Users {
			if id, err := uuid.Parse(raw); err == nil {
				policy.AllowUsers = append(policy.AllowUsers, id)
			}
		}
	}
	policy.NoEveryone = !slices.Contains(am.Parse, "everyone")
	if slices.Contains(am.Parse, "roles") && len(am.Roles) > 0 {
		return formError("allowed_mentions: roles cannot be in parse and listed at once")
	}
	return nil
}
