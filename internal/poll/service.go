package poll

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// votesPerMinute bounds one member's vote changes: each one is broadcast to
// the channel.
const votesPerMinute = 60

// createsPerMinute bounds how many polls one member starts: each is a message broadcast to the channel.
const createsPerMinute = 10

// Service creates, votes on and ends polls.
type Service struct {
	DB     *db.Pool
	Perms  *perm.Service
	Events events.Publisher
	// Online resolves @here in the question; nil means nobody is online.
	Online chat.OnlineSource
	// Audit records a moderator ending someone else's poll; nil records nothing.
	Audit *audit.Log

	votes   *httpx.RateLimiter
	creates *httpx.RateLimiter
}

// NewService returns s with its state.
func NewService(s Service) *Service {
	s.votes = httpx.NewRateLimiter(votesPerMinute, time.Minute).Named("poll_vote")
	s.creates = httpx.NewRateLimiter(createsPerMinute, time.Minute).Named("poll_create")
	return &s
}

// Create posts a poll to the channel as user and announces it like any
// message. ctx comes from chat.SendContext, which already checked that the
// user may send there and carries the mention policy for the question.
func (s *Service) Create(ctx context.Context, user *auth.User, channel uuid.UUID, in Input) (*chat.Message, error) {
	c, err := in.clean()
	if err != nil {
		return nil, err
	}
	if _, err := chat.ChannelForMessages(ctx, s.DB, channel); err != nil {
		return nil, err
	}
	id, err := chat.InsertMessage(ctx, s.DB, s.Online, chat.NewMessage{ChannelID: channel, UserID: user.ID, Content: c.Question},
		func(tx pgx.Tx, messageID uuid.UUID) error { return insertPoll(ctx, tx, messageID, c) })
	if err != nil {
		return nil, err
	}
	msg, err := chat.GetMessage(ctx, s.DB, id)
	if err != nil {
		return nil, err
	}
	s.Events.BroadcastChannel(channel, events.MessageCreate, msg)
	return msg, nil
}

// Update is the payload of poll_update: the poll's tallies after a vote or
// the end, for everyone who can view the channel. It is the same for all of
// them, so it carries no voter of an anonymous poll and no one's own votes.
type Update struct {
	MessageID uuid.UUID `json:"message_id" format:"uuid"`
	ChannelID uuid.UUID `json:"channel_id" format:"uuid"`
	Poll      chat.Poll `json:"poll"`
}

func (s *Service) announce(ctx context.Context, channel, messageID uuid.UUID) (*chat.Poll, error) {
	p, err := chat.LoadPoll(ctx, s.DB, messageID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, errPollNotFound
	}
	s.Events.BroadcastChannel(channel, events.PollUpdate, Update{MessageID: messageID, ChannelID: channel, Poll: *p})
	return p, nil
}

// Detail is a poll with the caller's own votes.
type Detail struct {
	chat.Poll
	// MyVotes are the options the caller voted for (also in an anonymous poll).
	MyVotes []uuid.UUID `json:"my_votes" format:"uuid"`
}

func (s *Service) detail(ctx context.Context, messageID, userID uuid.UUID) (Detail, error) {
	p, err := chat.LoadPoll(ctx, s.DB, messageID)
	if err != nil {
		return Detail{}, err
	}
	if p == nil {
		return Detail{}, errPollNotFound
	}
	mine, err := MyVotes(ctx, s.DB, messageID, userID)
	return Detail{Poll: *p, MyVotes: mine}, err
}

// find resolves {channelID}/{messageID} of a request to a poll the caller may
// see: without need in the channel the poll does not exist for them.
func (s *Service) find(r *http.Request, need perm.Set) (ref, error) {
	ch, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return ref{}, err
	}
	msg, err := httpx.PathUUID(r, "messageID")
	if err != nil {
		return ref{}, err
	}
	if err := s.Perms.RequireChannel(r.Context(), auth.UserFrom(r.Context()).Subject(), ch, need); err != nil {
		return ref{}, err
	}
	rf, err := loadRef(r.Context(), s.DB, msg)
	if err != nil {
		return ref{}, err
	}
	if rf.channel != ch {
		return ref{}, errPollNotFound
	}
	return rf, nil
}
