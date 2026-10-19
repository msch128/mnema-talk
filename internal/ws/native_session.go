package ws

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/internal/sfu"
	"github.com/pion/webrtc/v4"
)

// nativeSession is the desktop lease stored on a socket. The zero value is a
// browser socket: there is no lease, and commandsAllowed stays true.
type nativeSession struct {
	native *nativeSocketState
}

// hasNativeLease reports whether this socket holds a desktop lease.
func (c *Client) hasNativeLease() bool {
	return c.native != nil
}

// leaseSealed reports whether a leased socket was sealed.
// A socket without a lease is never sealed.
func (c *Client) leaseSealed() bool {
	return c.native != nil && c.native.securityClosed.Load()
}

// sameNativeFamily reports whether other belongs to c's credential family.
// binding does not change after admission; callers hold Hub.mu.
func (c *Client) sameNativeFamily(other *Client) bool {
	return c.native != nil && other.native != nil && other.native.binding.FamilyID() == c.native.binding.FamilyID()
}

// nativeFamilyCountLocked counts the open and pending connections of c's
// native credential family. Callers hold h.mu.
func (h *Hub) nativeFamilyCountLocked(c *Client) int {
	count := 0
	for _, set := range []map[*Client]struct{}{h.clients, h.pending} {
		for existing := range set {
			if c.sameNativeFamily(existing) {
				count++
			}
		}
	}
	return count
}

// cancelNativeSession stops the lease context. A socket without a lease has none.
func (c *Client) cancelNativeSession() {
	if c.native != nil {
		c.native.cancel()
	}
}

// commandContext is the lease context, or Background when there is no lease.
// Handlers bound their work with it so sealing the lease cancels them.
func (c *Client) commandContext() context.Context {
	if c.native == nil {
		return context.Background()
	}
	return c.native.ctx
}

// rejectNativeBinary ends a leased socket that sent anything but a text frame.
// A socket without a lease is left to the browser pumps.
func (c *Client) rejectNativeBinary(messageType int) bool {
	if c.native == nil || messageType == websocket.TextMessage {
		return false
	}
	c.hub.terminateNativeClient(c)
	return true
}

// leaseWriteDeadline caps fallback at the lease deadline. Without a lease
// the deadline stays fallback.
func (c *Client) leaseWriteDeadline(fallback time.Time) time.Time {
	if c.native == nil {
		return fallback
	}
	_, _, leaseDeadline := c.native.snapshot()
	return minTime(fallback, leaseDeadline)
}

// joinLeasedVoice joins a leased socket through JoinWithAuthorization.
// commandsAllowed is the authorization callback. Without a lease, leased is
// false and the caller uses the ordinary SFU join.
func (c *Client) joinLeasedVoice(
	forward *sfu.SFU, channelID uuid.UUID,
	sendOffer func(webrtc.SessionDescription), sendICE func(*webrtc.ICECandidateInit),
) (*sfu.Peer, error, bool) {
	if c.native == nil {
		return nil, nil, false
	}
	_, peer, err := forward.JoinWithAuthorization(channelID, c.User.ID, sendOffer, sendICE, c.commandsAllowed)
	return peer, err, true
}
