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

func (c *Client) nativeLive() bool {
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
func (c *Client) emitApplicationEvent(eventType string, payload any, excludeSelf bool, voice *uuid.UUID) {
	h := c.hub
	if c.native == nil {
		switch {
		case excludeSelf:
			h.broadcastExcept(c.User.ID, eventType, payload)
		case voice != nil:
			h.sendToVoiceRoom(*voice, eventType, payload)
		default:
			h.Broadcast(eventType, payload)
		}
		return
	}
	data := encode(eventType, payload)
	if data == nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if !c.nativeLive() {
		return
	}
	for recipient := range h.clients {
		if excludeSelf && recipient.User.ID == c.User.ID {
			continue
		}
		if voice != nil {
			current := recipient.currentVoice()
			if current == nil || *current != *voice {
				continue
			}
		}
		recipient.deliver(data)
	}
}
