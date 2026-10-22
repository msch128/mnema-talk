package push

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/metrics"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Snapshots is the permission service as push needs it.
type Snapshots interface {
	Snapshot(ctx context.Context) (*perm.Snapshot, error)
}

// Lifetimes: a message that mentions the member waits a day for an offline
// device, any other one an hour (it is stale by then).
const (
	mentionTTL = 24 * time.Hour
	messageTTL = time.Hour
	testTTL    = time.Minute
	// testBudget bounds a whole test push, whatever the number of devices.
	testBudget = 25 * time.Second

	sendTimeout   = 10 * time.Second
	retryWait     = 2 * time.Second
	maxRetryDelay = 5 * time.Second

	sendWorkers  = 8
	queueSize    = 512
	sendQueue    = 1024
	sweepEvery   = time.Minute
	defaultBurst = 5 * time.Second
)

// Service decides who is told about a new message and hands the pushes to a
// small pool of senders. It never blocks the hub: events and sends are queued
// in bounded buffers and dropped, counted, when they overflow.
type Service struct {
	DB     *db.Pool
	Perms  Snapshots
	Sender *Sender
	// Previews is false when the operator forbids message text in pushes.
	Previews bool
	// Burst is the coalescing window; zero means five seconds.
	Burst time.Duration
	// RetryWait is how long a temporary refusal waits before the one retry;
	// zero means two seconds.
	RetryWait time.Duration
	// Now is the clock; nil means time.Now.
	Now func() time.Time

	once     sync.Once
	messages chan chat.Message
	jobs     chan job
	co       *coalescer
	ctx      context.Context
}

type job struct {
	target  target
	payload []byte
	opts    Options
}

func (s *Service) init() {
	s.once.Do(func() {
		if s.Now == nil {
			s.Now = time.Now
		}
		burst := s.Burst
		if burst <= 0 {
			burst = defaultBurst
		}
		s.messages = make(chan chat.Message, queueSize)
		s.jobs = make(chan job, sendQueue)
		s.co = newCoalescer(burst, s.Now)
	})
}

// Observe is the hub's tap: it queues a new message for delivery. It never blocks.
func (s *Service) Observe(_ uuid.UUID, eventType string, payload any) {
	if eventType != events.MessageCreate {
		return
	}
	var msg chat.Message
	switch v := payload.(type) {
	case chat.Message:
		msg = v
	case *chat.Message:
		msg = *v
	default:
		return
	}
	s.init()
	select {
	case s.messages <- msg:
	default:
		metrics.PushDeliveries.With("dropped").Inc()
	}
}

// Run delivers until ctx ends.
func (s *Service) Run(ctx context.Context) {
	s.init()
	s.ctx = ctx
	defer s.co.stop()
	var wg sync.WaitGroup
	for range sendWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.sendLoop(ctx)
		}()
	}
	sweep := time.NewTicker(sweepEvery)
	defer sweep.Stop()
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case msg := <-s.messages:
			s.handle(ctx, msg)
		case <-sweep.C:
			s.co.sweep()
			s.pruneEnded(ctx)
		}
	}
}

func (s *Service) sendLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case j := <-s.jobs:
			s.deliver(ctx, j)
		}
	}
}

// mentions reports whether msg mentions the member or replies to them.
func mentions(msg chat.Message, user uuid.UUID) bool {
	return slices.Contains(msg.Mentions, user) || msg.ReplyTo != nil && msg.ReplyTo.UserID == user
}

// handle works out who is told about msg and queues their pushes.
func (s *Service) handle(ctx context.Context, msg chat.Message) {
	cands, err := loadCandidates(ctx, s.DB, msg.ChannelID, msg.UserID, nil)
	if err != nil || len(cands) == 0 {
		if err != nil {
			slog.Warn("push: load recipients", "err", err)
		}
		return
	}
	snap, err := s.Perms.Snapshot(ctx)
	if err != nil {
		slog.Warn("push: permission snapshot", "err", err)
		return // fail closed: nobody is told what they may not see
	}
	var chosen []candidate
	for _, c := range cands {
		if canView(snap, c.userID, msg.ChannelID) && c.wants(mentions(msg, c.userID)) {
			chosen = append(chosen, c)
		}
	}
	s.notify(ctx, msg, chosen)
}

// notify sends (or holds, see coalescer) the push of msg to each chosen member.
func (s *Service) notify(ctx context.Context, msg chat.Message, chosen []candidate) {
	if len(chosen) == 0 {
		return
	}
	ids := make([]uuid.UUID, len(chosen))
	for i, c := range chosen {
		ids[i] = c.userID
	}
	targets, err := targetsFor(ctx, s.DB, ids)
	if err != nil {
		slog.Warn("push: load subscriptions", "err", err)
		return
	}
	for _, c := range chosen {
		h := held{
			user: c.userID, locale: c.locale, preview: c.preview && s.Previews,
			notice: Notice{
				ChannelID: msg.ChannelID, ChannelName: c.channelName, MessageID: msg.ID, ParentID: msg.ParentID,
				Sender: sender(msg), Content: msg.Content, HasFiles: len(msg.Attachments) > 0 || len(msg.Embeds) > 0,
				Mention: mentions(msg, c.userID), Count: 1, At: s.Now(),
			},
		}
		if now := s.co.add(key{c.userID, msg.ChannelID}, h, s.flush); now != nil {
			s.enqueue(*now, targets[c.userID])
		}
	}
}

func sender(msg chat.Message) string {
	if msg.DisplayName != "" {
		return msg.DisplayName
	}
	return msg.Username
}

// flush sends what a window held back; its targets are read again, since
// devices may have come and gone meanwhile. Access, mute and notification
// level are checked again too: a member who lost the channel or muted it
// inside the window is not told.
func (s *Service) flush(h held) {
	ctx, cancel := context.WithTimeout(s.ctx, sendTimeout)
	defer cancel()
	if !s.stillQualifies(ctx, h) {
		metrics.PushDeliveries.With("dropped").Inc()
		return
	}
	targets, err := targetsFor(ctx, s.DB, []uuid.UUID{h.user})
	if err != nil {
		slog.Warn("push: load subscriptions", "err", err)
		return
	}
	s.enqueue(h, targets[h.user])
}

// stillQualifies repeats the message-time checks for a held push. It fails
// closed: when the state cannot be read, nothing is sent.
func (s *Service) stillQualifies(ctx context.Context, h held) bool {
	cands, err := loadCandidates(ctx, s.DB, h.notice.ChannelID, uuid.Nil, &h.user)
	if err != nil {
		slog.Warn("push: recheck recipient", "err", err)
		return false
	}
	snap, err := s.Perms.Snapshot(ctx)
	if err != nil {
		slog.Warn("push: permission snapshot", "err", err)
		return false
	}
	return len(cands) == 1 && canView(snap, h.user, h.notice.ChannelID) && cands[0].wants(h.notice.Mention)
}

// enqueue builds the push and queues one job per device.
func (s *Service) enqueue(h held, targets []target) {
	body, err := build(h.notice, h.locale, h.preview).encode()
	if err != nil {
		slog.Warn("push: encode payload", "err", err)
		return
	}
	opts := Options{TTL: messageTTL, Urgent: h.notice.Mention, Topic: topic(h.notice.ChannelID, h.notice.Mention), Timeout: sendTimeout}
	if h.notice.Mention {
		opts.TTL = mentionTTL
	}
	for _, t := range targets {
		select {
		case s.jobs <- job{target: t, payload: body, opts: opts}:
		default:
			metrics.PushDeliveries.With("dropped").Inc()
		}
	}
}

// topic collapses pushes of one channel that wait for an offline device (RFC
// 8030 section 5.4): only the newest is delivered. Mentions have their own
// topic so a later plain message never replaces one.
func topic(channel uuid.UUID, mention bool) string {
	label := channel.String()
	if mention {
		label += ":mention"
	}
	sum := sha256.Sum256([]byte(label))
	return base64.RawURLEncoding.EncodeToString(sum[:16])
}

// deliver sends one job, retrying once after a temporary refusal, and records
// what came of it.
func (s *Service) deliver(ctx context.Context, j job) {
	res := s.Sender.Send(ctx, j.target, j.payload, j.opts)
	if res.Outcome == Retry {
		wait := s.RetryWait
		if wait <= 0 {
			wait = retryWait
		}
		wait = min(max(res.RetryAfter, wait), maxRetryDelay)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		res = s.Sender.Send(ctx, j.target, j.payload, j.opts)
	}
	s.record(ctx, j.target.id, res)
}

// record keeps the subscription's books: delivered resets the failure count,
// gone removes it, anything else counts toward its removal.
func (s *Service) record(ctx context.Context, id uuid.UUID, res Result) {
	metrics.PushDeliveries.With(string(res.Outcome)).Inc()
	var err error
	switch res.Outcome {
	case Delivered:
		err = recordSuccess(ctx, s.DB, id)
	case Gone:
		err = recordGone(ctx, s.DB, id)
	default:
		slog.Debug("push: not delivered", "subscription", id, "outcome", res.Outcome, "reason", res.Reason)
		err = recordFailure(ctx, s.DB, id)
	}
	if err != nil && ctx.Err() == nil {
		slog.Warn("push: record result", "subscription", id, "err", err)
	}
}

// pruneEnded removes the devices of sessions that were signed out or expired,
// or that a password change or "log out everywhere" revoked. They are never
// told anything meanwhile (every query checks the session), this only tidies up.
func (s *Service) pruneEnded(ctx context.Context) {
	if n, err := pruneEnded(ctx, s.DB); err != nil && ctx.Err() == nil {
		slog.Warn("push: prune ended sessions", "err", err)
	} else if n > 0 {
		slog.Debug("push: removed devices of ended sessions", "count", n)
	}
}
