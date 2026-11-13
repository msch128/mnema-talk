// Package expiry deletes messages at the end of their lifetime
// (doc/timed-messages.md). The deletion is the one of an author's own delete
// (messages.DeleteExpired); this package only decides when.
package expiry

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/chat/readstate"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/events/wire"
	"github.com/msch128/mnema-talk/internal/worker"
)

const (
	// batch is how many ended messages one pass reads; each is deleted in a
	// transaction of its own.
	batch = 100
	// idle is the longest sleep without a known end: a safety net, the
	// sweeper is woken by every new message with a lifetime.
	idle = 10 * time.Minute
	// minSleep keeps a database clock that lags the server's from turning the
	// loop into a spin: the end is read again at most this often.
	minSleep = 100 * time.Millisecond
	// retry is the pause after a failed pass.
	retry = 5 * time.Second
)

// Config is what a sweeper works with.
type Config struct {
	DB     *db.Pool
	Events events.Publisher
	// Objects deletes the attachments' stored files; nil leaves them.
	Objects core.ObjectDeleter
	// Recount tells the members who had not read a removed message their
	// counts again; nil tells nobody.
	Recount *readstate.Recounter
}

// Sweeper deletes the messages whose lifetime ended. It sleeps until the next
// end and wakes early when a message with an earlier end is stored.
type Sweeper struct {
	Config
	wake chan struct{}
	// target is when the loop wakes next, in Unix nanoseconds; 0 while a
	// pass runs, so a message stored meanwhile always signals.
	target atomic.Int64
}

// New returns a sweeper that the message door of its database wakes.
func New(c Config) *Sweeper {
	out := &Sweeper{Config: c, wake: make(chan struct{}, 1)}
	messages.OnExpiry(c.DB, out.Wake)
	return out
}

// Wake tells the loop about a message that ends at at. It returns at once and
// signals only when that is earlier than the loop's next wake.
func (s *Sweeper) Wake(at time.Time) {
	if t := s.target.Load(); t != 0 && at.UnixNano() >= t {
		return
	}
	select {
	case s.wake <- struct{}{}:
	default: // a signal is pending already
	}
}

// Run removes what ended while the server was down, then sleeps until each
// next end, until ctx ends.
func (s *Sweeper) Run(ctx context.Context) {
	t := time.NewTimer(0)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-s.wake:
			if !t.Stop() {
				select {
				case <-t.C:
				default:
				}
			}
		}
		s.target.Store(0)
		sleep := s.pass(ctx)
		s.target.Store(time.Now().Add(sleep).UnixNano())
		t.Reset(sleep)
	}
}

// pass sweeps once and returns how long to sleep.
func (s *Sweeper) pass(ctx context.Context) time.Duration {
	var next time.Time
	var failed bool
	worker.Tick(worker.JobMessageExpiry, func() error {
		// An online backup freezes the storage: wait for it to end. Reads
		// leave ended messages out meanwhile.
		err := s.DB.OutsideBackup(ctx, func() error {
			var err error
			next, err = s.Sweep(ctx)
			return err
		})
		if err != nil && ctx.Err() == nil {
			slog.Error("message expiry sweep failed", "err", err)
			failed = true
		}
		return worker.IgnoreDone(ctx, err)
	})
	switch {
	case failed:
		return retry
	case next.IsZero():
		return idle
	}
	return min(max(time.Until(next), minSleep), idle)
}

// Sweep deletes every message whose lifetime ended, in batches, and returns
// the next end (zero when no message has a lifetime).
func (s *Sweeper) Sweep(ctx context.Context) (time.Time, error) {
	if err := s.RemoveEnded(ctx); err != nil {
		return time.Time{}, err
	}
	return s.NextEnd(ctx)
}

// RemoveEnded deletes every message whose lifetime ended, in batches.
func (s *Sweeper) RemoveEnded(ctx context.Context) error {
	for {
		ids, err := s.ended(ctx)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if err := s.remove(ctx, id); err != nil {
				return err
			}
		}
		if len(ids) < batch {
			return nil
		}
	}
}

// NextEnd is the earliest end of a message still stored, zero for none.
func (s *Sweeper) NextEnd(ctx context.Context) (time.Time, error) {
	var next *time.Time
	if err := s.DB.QueryRow(ctx, `SELECT min(expires_at) FROM messages WHERE expires_at IS NOT NULL`).Scan(&next); err != nil {
		return time.Time{}, err
	}
	if next == nil {
		return time.Time{}, nil
	}
	return *next, nil
}

// ended reads the next batch of ended messages, oldest end first.
func (s *Sweeper) ended(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT id FROM messages WHERE expires_at IS NOT NULL AND expires_at <= clock_timestamp()
		ORDER BY expires_at LIMIT $1`, batch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// remove deletes one ended message as its author's delete would, then tells
// the channel and corrects the counts. A message that is gone already (its
// author or a moderator was faster, or its thread root went) is no error.
func (s *Sweeper) remove(ctx context.Context, id uuid.UUID) error {
	d, err := messages.DeleteExpired(ctx, s.DB, id)
	if err != nil || d == nil {
		return err
	}
	core.DeleteObjects(ctx, s.Objects, d.Keys)
	if s.Events != nil {
		s.Events.BroadcastChannel(d.Channel, wire.MessageDelete{ID: id, ChannelID: d.Channel, ParentID: d.Parent})
	}
	s.Recount.Tell(d.Channel, &d.Newest, nil)
	return nil
}
