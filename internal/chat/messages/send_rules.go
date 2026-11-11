package messages

import (
	"context"
	"time"

	"github.com/msch128/mnema-talk/internal/chat/mentions"
)

// sendRules is what the author's permissions mean for one new message. The
// gate decides them before the insert, the insert applies them in its
// transaction.
type sendRules struct {
	// SlowModeExempt is set for an author who may manage messages in the
	// channel: neither slow mode nor a locked post holds them back.
	SlowModeExempt bool
	// NewMemberWait is how long the author still waits as a new member before
	// they may write first in a conversation (raid protection); zero: not at all.
	NewMemberWait time.Duration
	// Mentions limits whom the message notifies.
	Mentions mentions.MentionPolicy
}

// rulesFrom reads the rules from the context values that the entry points
// outside Service.Post still use (uploads, forwards, forum posts, polls,
// invitations, webhooks, interaction responses). Service.Post passes them as a value and
// reads none of these.
func rulesFrom(ctx context.Context) sendRules {
	return sendRules{SlowModeExempt: slowModeExempt(ctx), NewMemberWait: newMemberWriteWait(ctx), Mentions: mentions.PolicyFrom(ctx)}
}

type newMemberWriteWaitKey struct{}

// withNewMemberWriteWait carries how long the author still waits as a new
// member before they may write (raid protection); zero: not at all.
func withNewMemberWriteWait(ctx context.Context, wait time.Duration) context.Context {
	return context.WithValue(ctx, newMemberWriteWaitKey{}, wait)
}

func newMemberWriteWait(ctx context.Context) time.Duration {
	v, _ := ctx.Value(newMemberWriteWaitKey{}).(time.Duration)
	return v
}

type slowModeExemptKey struct{}

// WithSlowModeExempt marks a request whose author is not held back by slow
// mode (they may manage messages in the channel).
func WithSlowModeExempt(ctx context.Context, exempt bool) context.Context {
	return context.WithValue(ctx, slowModeExemptKey{}, exempt)
}

func slowModeExempt(ctx context.Context) bool {
	v, _ := ctx.Value(slowModeExemptKey{}).(bool)
	return v
}
