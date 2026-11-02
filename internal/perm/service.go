package perm

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

const (
	// snapshotTTL is the backstop for a change that bypassed Invalidate (an
	// operator editing the database); every API change invalidates at once.
	snapshotTTL = time.Minute
	// missReload is the shortest gap between reloads forced by a channel the
	// snapshot has not seen yet, so unknown IDs cannot hammer the database.
	missReload = time.Second
)

// Notifier reacts to permission changes (implemented by the WebSocket hub):
// it re-checks live connections. nil userIDs means everybody.
type Notifier interface {
	PermissionsChanged(userIDs []uuid.UUID)
}

// Service answers permission questions from an in-memory Snapshot of the
// database that is rebuilt after every change. A database failure fails the
// question (and so the request) closed.
type Service struct {
	db   *db.Pool // nil for a Service built by NewServiceFrom
	load Loader
	now  func() time.Time

	mu         sync.Mutex // serializes loads
	lastForced time.Time  // guarded by mu
	cur        atomic.Pointer[Snapshot]
	prev       atomic.Pointer[Snapshot] // the snapshot in force before the latest Invalidate
	gen        atomic.Uint64

	uploadCeilingMB atomic.Int64

	notifierMu sync.RWMutex
	notifier   Notifier
}

// Loader builds the permission state as of now. The database is the only
// production source; tests plug in a hand-built state.
type Loader func(ctx context.Context, now time.Time) (*Snapshot, error)

// NewService returns a Service reading from p. It panics on a nil pool: a
// server without a database has no permissions to answer from.
func NewService(p *db.Pool) *Service {
	if p == nil {
		panic("perm: NewService needs a database pool")
	}
	return &Service{db: p, now: time.Now, load: func(ctx context.Context, now time.Time) (*Snapshot, error) {
		return loadSnapshot(ctx, p, now)
	}}
}

// NewServiceFrom returns a Service that loads its state from load instead of
// the database. It cannot run Locked changes. For tests (see permtest).
func NewServiceFrom(load Loader) *Service { return &Service{load: load, now: time.Now} }

// SetNotifier registers who learns about permission changes.
func (s *Service) SetNotifier(n Notifier) {
	s.notifierMu.Lock()
	s.notifier = n
	s.notifierMu.Unlock()
}

// SetUploadCeiling sets the operator's hard upload ceiling (MAX_UPLOAD_SIZE_MB).
func (s *Service) SetUploadCeiling(mb int) { s.uploadCeilingMB.Store(int64(mb)) }

// UploadCeilingMB is the operator's upload ceiling in MB (50 when unset).
func (s *Service) UploadCeilingMB() int {
	if v := int(s.uploadCeilingMB.Load()); v > 0 {
		return v
	}
	return 50
}

// UploadLimitBytes is the largest file sub may upload: the Owner gets the
// ceiling, others the highest override of their roles or the global limit,
// never above the ceiling.
func (s *Service) UploadLimitBytes(ctx context.Context, sub Subject) (int64, error) {
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return 0, err
	}
	return int64(snap.UploadMB(sub, s.UploadCeilingMB())) << 20, nil
}

// GlobalUploadLimitBytes is the limit for requests without a member behind
// them (webhooks).
func (s *Service) GlobalUploadLimitBytes(ctx context.Context) (int64, error) {
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return 0, err
	}
	return int64(snap.GlobalUploadMB(s.UploadCeilingMB())) << 20, nil
}

// UploadSettings is the global upload limit as the Owner sees it.
type UploadSettings struct {
	// MaxUploadMB is the effective global limit.
	MaxUploadMB int `json:"max_upload_mb" minimum:"1"`
	// CeilingMB is the operator's hard ceiling (MAX_UPLOAD_SIZE_MB).
	CeilingMB int `json:"ceiling_mb" minimum:"1"`
}

// UploadSettings returns the effective global limit and the ceiling.
func (s *Service) UploadSettings(ctx context.Context) (UploadSettings, error) {
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return UploadSettings{}, err
	}
	c := s.UploadCeilingMB()
	return UploadSettings{MaxUploadMB: snap.GlobalUploadMB(c), CeilingMB: c}, nil
}

// SetGlobalUploadMB stores the global upload limit (nil = back to the
// ceiling). Owner only; everybody's effective limit may change.
func (s *Service) SetGlobalUploadMB(ctx context.Context, actor Subject, mb *int) (UploadSettings, error) {
	if err := RequireOwner(actor); err != nil {
		return UploadSettings{}, err
	}
	if mb != nil && (*mb < 1 || *mb > s.UploadCeilingMB()) {
		return UploadSettings{}, httpx.ErrInvalidInput(fmt.Sprintf("max_upload_mb must be between 1 and %d", s.UploadCeilingMB()))
	}
	err := s.Locked(ctx, actor, func(tx pgx.Tx, _ *Snapshot) error {
		_, err := tx.Exec(ctx, `UPDATE instance_settings SET max_upload_mb = $1, updated_at = NOW()`, mb)
		return err
	})
	if err != nil {
		return UploadSettings{}, err
	}
	s.Changed(nil)
	return s.UploadSettings(ctx)
}

// Snapshot returns the current permission state, loading it when it is
// missing or older than the TTL.
func (s *Service) Snapshot(ctx context.Context) (*Snapshot, error) {
	if cur := s.cur.Load(); cur != nil && cur.age(s.now()) < snapshotTTL {
		return cur, nil
	}
	return s.reload(ctx, false)
}

func (s *Service) reload(ctx context.Context, forced bool) (*Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if snap := s.reuseCurrentSnapshot(now, forced); snap != nil {
		return snap, nil
	}
	if forced {
		s.lastForced = now
	}
	gen := s.gen.Load()
	snap, err := s.load(ctx, now)
	if err != nil {
		return nil, err
	}
	// A change that happened while loading may not be in snap: serve it to
	// this caller but do not keep it.
	// Invalidate does not take s.mu, so re-check after storing and take the
	// snapshot back if a change slipped in between the check and the store.
	s.cur.Store(snap)
	if s.gen.Load() != gen {
		s.cur.CompareAndSwap(snap, nil)
	}
	return snap, nil
}

func (s *Service) reuseCurrentSnapshot(now time.Time, forced bool) *Snapshot {
	cur := s.cur.Load()
	if cur == nil || cur.age(now) >= snapshotTTL {
		return nil
	}
	if !forced || now.Sub(s.lastForced) < missReload {
		return cur
	}
	return nil
}

// Invalidate drops the snapshot; the next question reloads it.
func (s *Service) Invalidate() {
	if s == nil {
		return
	}
	s.gen.Add(1)
	if old := s.cur.Swap(nil); old != nil {
		s.prev.Store(old)
	}
}

// Previous returns the snapshot that was current before the latest change, or
// nil when none was loaded. Notifiers compare it with the new one to find out
// what a change took away from whom.
func (s *Service) Previous() *Snapshot {
	if s == nil {
		return nil
	}
	return s.prev.Load()
}

// Changed invalidates the snapshot and tells the notifier that the given
// users' permissions (everybody's when nil) may have changed.
func (s *Service) Changed(userIDs []uuid.UUID) {
	if s == nil {
		return
	}
	s.Invalidate()
	s.notifierMu.RLock()
	n := s.notifier
	s.notifierMu.RUnlock()
	if n != nil {
		n.PermissionsChanged(userIDs)
	}
}

// ForChannel returns a snapshot that knows ch, reloading once (rate-limited)
// when the channel is new to the cached one. A channel that still is not
// known does not exist.
func (s *Service) ForChannel(ctx context.Context, ch uuid.UUID) (*Snapshot, error) {
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	if !snap.KnowsChannel(ch) && !snap.KnowsDM(ch) {
		return s.reload(ctx, true)
	}
	return snap, nil
}

// Global is what sub may do server-wide. A nil Service imposes no limits (for
// code paths and tests that run without the permission model; the router
// always builds one).
func (s *Service) Global(ctx context.Context, sub Subject) (Set, error) {
	if s == nil {
		return All, nil
	}
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return 0, err
	}
	return snap.Global(sub), nil
}

// Channel is what sub may do in channel ch.
func (s *Service) Channel(ctx context.Context, sub Subject, ch uuid.UUID) (Set, error) {
	if s == nil {
		return All, nil
	}
	snap, err := s.ForChannel(ctx, ch)
	if err != nil {
		return 0, err
	}
	return snap.Channel(sub, ch), nil
}

// ErrChannelHidden is what a channel the caller may not view answers: the
// same 404 as a channel that does not exist, so hidden channels stay hidden.
var ErrChannelHidden = httpx.ErrNotFound("channel not found")

// RequireChannel checks that sub holds need in ch. Without ViewChannel the
// channel looks nonexistent (404); a missing permission otherwise is a 403.
func (s *Service) RequireChannel(ctx context.Context, sub Subject, ch uuid.UUID, need Set) error {
	if s == nil {
		return nil
	}
	snap, err := s.ForChannel(ctx, ch)
	if err != nil {
		return err
	}
	return snap.Deny(sub, snap.Channel(sub, ch), need)
}

// CheckChannel is RequireChannel for an already resolved Set.
func CheckChannel(p, need Set) error {
	if !p.Has(ViewChannel) {
		return ErrChannelHidden
	}
	if name := p.MissingName(need); name != "" {
		return httpx.ErrForbidden("missing permission: " + name)
	}
	return nil
}

// RequireGlobal checks that sub holds need server-wide.
func (s *Service) RequireGlobal(ctx context.Context, sub Subject, need Set) error {
	if s == nil {
		return nil
	}
	snap, err := s.Snapshot(ctx)
	if err != nil {
		return err
	}
	p := snap.Global(sub)
	if name := p.MissingName(need); name != "" {
		if snap.RulesPending(sub.ID) && need&^p&TimeoutMask != 0 {
			return ErrRulesNotAccepted
		}
		if nm := snap.newMemberRefusal(sub.ID, p, need); nm != nil {
			return nm
		}
		return httpx.ErrForbidden("missing permission: " + name)
	}
	return nil
}

// RequireOwner is for the Owner-only powers that no permission grants.
func RequireOwner(sub Subject) error {
	if !sub.Owner {
		return httpx.ErrForbidden("only the Owner can do this")
	}
	return nil
}

var errNoDatabase = errors.New("permissions: service has no database")
