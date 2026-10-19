package ws

import (
	"context"
	"math"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/perm"
)

const nativeRevalidateEvery = 5 * time.Second

type nativeSocketState struct {
	// Immutable after pending registration; Hub.mu can inspect without mu.
	binding        auth.NativePrincipal
	authenticator  NativeAuthenticator
	ctx            context.Context
	cancel         context.CancelFunc
	securityClosed atomic.Bool
	// Published immutable monotonic deadline keeps the per-packet gate lock-free.
	liveDeadline   atomic.Pointer[time.Time]
	checking       atomic.Bool
	changed        chan struct{}
	mu             sync.Mutex
	lease          auth.NativeLease
	generation     uint64
	accessDeadline time.Time
	familyDeadline time.Time
	renewWindow    time.Time
	renewCount     int
}

func newNativeSocketState(a NativeAuthenticator, l auth.NativeLease) *nativeSocketState {
	ctx, cancel := context.WithCancel(context.Background())
	s := &nativeSocketState{binding: l.Principal(), authenticator: a, ctx: ctx, cancel: cancel, changed: make(chan struct{}, 1),
		lease: l, generation: 1, accessDeadline: l.AccessDeadline(), familyDeadline: l.FamilyDeadline()}
	deadline := minTime(l.Deadline(), s.accessDeadline, s.familyDeadline)
	s.liveDeadline.Store(&deadline)
	return s
}

func (s *nativeSocketState) snapshot() (uint64, auth.NativeLease, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.generation, s.lease, minTime(s.lease.Deadline(), s.accessDeadline, s.familyDeadline)
}

func minTime(first time.Time, others ...time.Time) time.Time {
	for _, other := range others {
		if other.Before(first) {
			first = other
		}
	}
	return first
}

// commandsAllowed reports whether this socket may still handle commands.
// A nil lease, which is every browser socket, stays allowed. A native
// socket is allowed only while its lease is unexpired and securityClosed
// is false.
func (c *Client) commandsAllowed() bool {
	if c.native == nil {
		return true
	}
	if c.closed.Load() || c.native.securityClosed.Load() {
		return false
	}
	deadline := c.native.liveDeadline.Load()
	return deadline != nil && time.Now().Before(*deadline) && !c.closed.Load() && !c.native.securityClosed.Load()
}

func (s *nativeSocketState) install(expected uint64, l auth.NativeLease, renew bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.securityClosed.Load() || s.ctx.Err() != nil || s.generation != expected || !s.binding.SameNativeBinding(l.Principal()) ||
		!time.Now().Before(minTime(s.lease.Deadline(), s.accessDeadline, s.familyDeadline)) {
		return false
	}
	if renew {
		if s.generation == math.MaxUint64 || l.Principal().AccessExpiresAt().Before(s.lease.Principal().AccessExpiresAt()) {
			return false
		}
		if !s.lease.Principal().SameNativeAccess(l.Principal()) {
			s.accessDeadline = l.AccessDeadline()
		}
		s.generation++
	} else if !s.lease.Principal().SameNativeAccess(l.Principal()) {
		return false
	}
	if !time.Now().Before(minTime(l.Deadline(), s.accessDeadline, s.familyDeadline)) {
		return false
	}
	s.lease = l
	deadline := minTime(l.Deadline(), s.accessDeadline, s.familyDeadline)
	s.liveDeadline.Store(&deadline)
	select {
	case s.changed <- struct{}{}:
	default:
	}
	return true
}

func (c *Client) nativeWatchdog() {
	s := c.native
	ticker := time.NewTicker(nativeRevalidateEvery)
	defer ticker.Stop()
	for {
		_, _, deadline := s.snapshot()
		timer := time.NewTimer(time.Until(deadline))
		select {
		case <-c.done:
			timer.Stop()
			return
		case <-s.changed:
			timer.Stop()
		case <-timer.C:
			if c.hub.expireNativeClient(c) {
				return
			}
		case <-ticker.C:
			timer.Stop()
			if s.checking.CompareAndSwap(false, true) {
				go c.revalidateNative()
			}
		}
	}
}

func (c *Client) revalidateNative() {
	s := c.native
	defer s.checking.Store(false)
	generation, old, _ := s.snapshot()
	fresh, err := s.authenticator.RevalidateNativeLease(s.ctx, old.Principal())
	current, _, _ := s.snapshot()
	if current != generation || c.closed.Load() {
		return
	}
	if err != nil || !s.install(generation, fresh, false) {
		c.hub.terminateNativeGeneration(c, generation)
	}
}

// sealNativeTransport never waits for voiceMu or Pion cleanup. Closing the
// underlying network transport first also unblocks a TLS data write without
// waiting for TLS close-notify serialization behind that write.
func sealNativeTransport(c *Client) {
	if c.native == nil {
		return
	}
	c.native.securityClosed.Store(true)
	c.shutdown()
	if c.conn != nil {
		transport := c.conn.UnderlyingConn()
		if tlsTransport, ok := transport.(interface{ NetConn() net.Conn }); ok {
			transport = tlsTransport.NetConn()
		}
		_ = transport.Close()
	}
	c.close()
}

func (h *Hub) terminateNativeClient(c *Client) {
	if c.native == nil {
		return
	}
	sealNativeTransport(c)
	h.leaveCurrentVoice(c)
}

// DisconnectNativeFamily applies confirmed commit notifications to the SAME
// Hub. Family isolation includes pending handshakes and exact peer ownership.
func (h *Hub) DisconnectNativeFamily(id uuid.UUID) {
	h.mu.Lock()
	var selected []*Client
	for c := range h.clients {
		if c.native != nil && c.native.binding.FamilyID() == id {
			c.native.securityClosed.Store(true)
			c.shutdown()
			selected = append(selected, c)
		}
	}
	for c := range h.pending {
		if c.native != nil && c.native.binding.FamilyID() == id {
			delete(h.pending, c)
			c.native.securityClosed.Store(true)
			c.shutdown()
			selected = append(selected, c)
		}
	}
	h.mu.Unlock()
	for _, c := range selected {
		sealNativeTransport(c)
	}
	for _, c := range selected {
		h.leaveCurrentVoice(c)
	}
}

// Closing a failed old query must be atomic with generation comparison.
func (h *Hub) terminateNativeGeneration(c *Client, expected uint64) {
	s := c.native
	s.mu.Lock()
	if s.generation != expected || c.closed.Load() {
		s.mu.Unlock()
		return
	}
	s.securityClosed.Store(true)
	s.mu.Unlock()
	h.terminateNativeClient(c)
}

func (h *Hub) expireNativeClient(c *Client) bool {
	s := c.native
	s.mu.Lock()
	if time.Now().Before(minTime(s.lease.Deadline(), s.accessDeadline, s.familyDeadline)) {
		s.mu.Unlock()
		return false
	}
	s.securityClosed.Store(true)
	s.mu.Unlock()
	h.terminateNativeClient(c)
	return true
}

// Native application-state publication is authorized after waiting on Hub.mu.
// The family observer seals under that same lock, so a frame that waited behind
// confirmed revocation cannot publish participant state. Browser calls delegate
// to their existing helpers, including their established presence behavior.
//
// view, when set, limits the event to the connections whose user may view
// that channel.
func (c *Client) emitApplicationEvent(eventType string, payload any, excludeSelf bool, voice *uuid.UUID, view uuid.UUID) {
	h := c.hub
	if c.native == nil && view == uuid.Nil && !excludeSelf {
		if voice != nil {
			h.sendToVoiceRoom(*voice, eventType, payload)
		} else {
			h.Broadcast(eventType, payload)
		}
		return
	}
	data := encode(eventType, payload)
	if data == nil {
		return
	}
	scope := eventScope{excludeUser: uuid.Nil, voice: voice, view: view}
	if excludeSelf {
		scope.excludeUser = c.User.ID
	}
	if view != uuid.Nil {
		var ok bool
		if scope.snap, ok = h.permSnapshot(view); !ok {
			return
		}
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if !c.commandsAllowed() {
		return
	}
	// Outgoing webhooks only hear what is really published: after the
	// publication is authorized and live, never before.
	if view != uuid.Nil {
		h.tapped(view, eventType, payload)
	}
	for recipient := range h.clients {
		if scope.admits(recipient) {
			recipient.deliver(data)
		}
	}
}

// eventScope narrows an application event to its audience.
type eventScope struct {
	excludeUser uuid.UUID      // uuid.Nil: nobody is left out
	voice       *uuid.UUID     // only connections in this voice room
	view        uuid.UUID      // only users who may view this channel
	snap        *perm.Snapshot // permissions to check view against
}

func (s eventScope) admits(recipient *Client) bool {
	if s.excludeUser != uuid.Nil && recipient.User.ID == s.excludeUser {
		return false
	}
	if s.voice != nil {
		current := recipient.currentVoice()
		if current == nil || *current != *s.voice {
			return false
		}
	}
	return s.view == uuid.Nil || recipient.can(s.snap, s.view, perm.ViewChannel)
}
