package readstate

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/events/wire"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/safego"
)

// Online reports who has an open connection right now (the WebSocket hub).
type Online interface {
	OnlineUserIDs() []uuid.UUID
}

const (
	// recountChannels bounds the channels waiting for a recount. A channel
	// already waiting takes a new request into its job (coalesced); a new
	// channel beyond the bound is dropped and logged: its members' badges
	// then heal at their next read, reconnect or reload, as before H-L5-01.
	recountChannels = 256
	// recountBatch is how many members one counting statement serves.
	recountBatch = 500
	// recountTimeout bounds one channel's recount.
	recountTimeout = 2 * time.Minute
)

// Recounter tells members their counts of a channel again after messages there
// were deleted or edited. The counts follow the messages that exist now and
// their current mentions, and neither a delete nor an edit brings a message a
// client could count, so the server, which knows the counts, sends them
// (read_state with unread_count, mention_count and the channel's
// last_message_number, which orders them against live message_create events).
//
// Tell only queues: one worker counts, off the request that changed the
// messages and after its broadcast, with the server's context (a request that
// ends does not stop it). Requests for a channel that is already waiting are
// merged into its job; the queue holds at most recountChannels channels and
// never blocks the caller. Once the server's context ends nothing new starts
// (Tell is refused, the worker stops before its next job). A nil Recounter
// tells nobody.
type Recounter struct {
	ctx    context.Context
	db     *db.Pool
	perms  *perm.Service
	online Online
	pub    events.Publisher

	mu      sync.Mutex
	jobs    map[uuid.UUID]*recountJob
	order   []uuid.UUID
	running bool
	wake    chan struct{}
	done    chan struct{}
}

type recountJob struct {
	// since: members who had read the channel up to it counted none of the
	// changed messages (nil: now).
	since *time.Time
	// users: the only members to tell (nil: every member online).
	users map[uuid.UUID]bool
}

// NewRecounter starts the worker; it runs until ctx ends.
func NewRecounter(ctx context.Context, p *db.Pool, perms *perm.Service, online Online, pub events.Publisher) *Recounter {
	perm.Must(perms, "read state recount")
	r := &Recounter{ctx: ctx, db: p, perms: perms, online: online, pub: pub,
		jobs: map[uuid.UUID]*recountJob{}, wake: make(chan struct{}, 1), done: make(chan struct{})}
	safego.Go("read_state_recount", safego.Crash, r.run)
	return r
}

// Tell queues a recount of channel for the members whose badge the change may
// have moved: those among candidates (every member online when nil) who are
// online, may view the channel and had not read it up to since (the newest
// changed message; nil means now).
func (r *Recounter) Tell(channel uuid.UUID, since *time.Time, candidates []uuid.UUID) {
	if r == nil || r.ctx.Err() != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if job, ok := r.jobs[channel]; ok {
		job.merge(since, candidates)
		return
	}
	if len(r.jobs) >= recountChannels {
		slog.Warn("read state recount: queue full, channel skipped", "channel", channel)
		return
	}
	job := &recountJob{since: since}
	if candidates != nil {
		job.users = map[uuid.UUID]bool{}
		job.merge(since, candidates)
	}
	r.jobs[channel] = job
	r.order = append(r.order, channel)
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// merge widens the job to cover another request: the later since, all users.
func (j *recountJob) merge(since *time.Time, users []uuid.UUID) {
	if j.since != nil && (since == nil || since.After(*j.since)) {
		j.since = since
	}
	if users == nil {
		j.users = nil
	}
	if j.users == nil {
		return
	}
	for _, id := range users {
		j.users[id] = true
	}
}

// Wait waits up to d for the worker to stop after the server's context ended.
func (r *Recounter) Wait(d time.Duration) bool {
	if r == nil {
		return true
	}
	select {
	case <-r.done:
		return true
	case <-time.After(d):
		return false
	}
}

// Idle reports whether no recount is waiting or running (tests wait for it).
func (r *Recounter) Idle() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.order) == 0 && !r.running
}

func (r *Recounter) run() {
	defer close(r.done)
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-r.wake:
		}
		for r.ctx.Err() == nil {
			channel, job, ok := r.next()
			if !ok {
				break
			}
			r.recount(channel, job)
			r.mu.Lock()
			r.running = false
			r.mu.Unlock()
		}
	}
}

func (r *Recounter) next() (uuid.UUID, *recountJob, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.order) == 0 {
		return uuid.Nil, nil, false
	}
	channel := r.order[0]
	r.order = r.order[1:]
	job := r.jobs[channel]
	delete(r.jobs, channel)
	r.running = true
	return channel, job, true
}

func (r *Recounter) recount(channel uuid.UUID, job *recountJob) {
	ctx, cancel := context.WithTimeout(r.ctx, recountTimeout)
	defer cancel()
	users := r.candidates(job)
	if len(users) == 0 {
		return
	}
	if err := r.tell(ctx, channel, job.since, users); err != nil && r.ctx.Err() == nil {
		slog.Warn("read state recount", "channel", channel, "err", err)
	}
}

// candidates keeps the job's users that have an open connection, each once.
func (r *Recounter) candidates(job *recountJob) []uuid.UUID {
	if r.online == nil {
		return nil
	}
	seen := map[uuid.UUID]bool{}
	var out []uuid.UUID
	for _, id := range r.online.OnlineUserIDs() {
		if seen[id] || (job.users != nil && !job.users[id]) {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func (r *Recounter) tell(ctx context.Context, channel uuid.UUID, since *time.Time, users []uuid.UUID) error {
	// Fail closed: without the current permissions nobody is told.
	snap, err := r.perms.Snapshot(ctx)
	if err != nil {
		return err
	}
	allowed := users[:0:0]
	for _, user := range users {
		if snap.Channel(perm.Subject{ID: user}, channel).Has(perm.ViewChannel) {
			allowed = append(allowed, user)
		}
	}
	for start := 0; start < len(allowed); start += recountBatch {
		batch := allowed[start:min(start+recountBatch, len(allowed))]
		states, err := ChannelReadStates(ctx, r.db, channel, since, batch)
		if err != nil {
			return err
		}
		for user, s := range states {
			r.pub.SendToUsers([]uuid.UUID{user}, wire.ReadStateCounts{
				ChannelID: channel, LastReadAt: s.LastReadAt, MentionCount: s.MentionCount,
				UnreadCount: s.UnreadCount, LastMessageNumber: s.LastMessageNumber,
			})
		}
	}
	return nil
}

// channelReadStatesSQL is ReadStateSQL for the members of $1 whose read marker
// in channel $3 (their sign-up when they never read it) lies before $4 (the
// database's now when null), one statement for them all: the same counts,
// muting and blocks as GET /api/read-state.
var channelReadStatesSQL = func() string {
	sql := ReadStateSQL()
	const where = "WHERE c.type <> 'dm'"
	if !strings.Contains(sql, where) || strings.Contains(sql, "$3") {
		panic("readstate: ReadStateSQL changed shape; channelReadStatesSQL must follow")
	}
	sql = strings.ReplaceAll(sql, "$1", "cand.id")
	sql = strings.Replace(sql, where, where+" AND c.id = $3", 1)
	return `
		SELECT cand.id, rs.* FROM unnest($1::uuid[]) AS cand(id)
		JOIN users u ON u.id = cand.id
		LEFT JOIN channel_reads cr0 ON cr0.user_id = cand.id AND cr0.channel_id = $3
		CROSS JOIN LATERAL (` + sql + `) rs
		WHERE COALESCE(cr0.last_read_at, u.created_at) < COALESCE($4, clock_timestamp())`
}()

// ChannelReadStates is GetReadStates of one channel for many members, keyed by
// member: those whose read marker lies before since (now when nil). A member
// without a read state there (a conversation, a channel that is gone) is left out.
func ChannelReadStates(ctx context.Context, q db.Querier, channel uuid.UUID, since *time.Time, users []uuid.UUID) (map[uuid.UUID]ReadState, error) {
	rows, err := q.Query(ctx, channelReadStatesSQL, users, core.MentionKindGroup, channel, since)
	if err != nil {
		return nil, fmt.Errorf("read states of a channel: %w", err)
	}
	out := map[uuid.UUID]ReadState{}
	_, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (struct{}, error) {
		var user uuid.UUID
		s, err := scanReadState(row, &user)
		out[user] = s
		return struct{}{}, err
	})
	if err != nil {
		return nil, fmt.Errorf("read states of a channel: %w", err)
	}
	return out, nil
}

// ChannelReadState is GetReadStates for one channel; false when the channel
// has no read state there (a conversation, or a channel that is gone).
func ChannelReadState(ctx context.Context, q db.Querier, userID, channelID uuid.UUID) (ReadState, bool, error) {
	states, err := ChannelReadStates(ctx, q, channelID, nil, []uuid.UUID{userID})
	if err != nil {
		return ReadState{}, false, err
	}
	s, ok := states[userID]
	return s, ok, nil
}
