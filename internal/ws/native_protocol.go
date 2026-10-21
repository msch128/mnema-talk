package ws

import (
	"strings"
	"time"
)

func (c *Client) handleNativeControl(eventType string, raw []byte) bool {
	if !strings.HasPrefix(eventType, "native_") {
		return false
	}
	if c.native == nil {
		return true
	} // Browser cannot use native controls.
	if !c.renewNativeLease(raw) {
		c.hub.terminateNativeClient(c)
	}
	return true
}

func (s *nativeSocketState) allowRenew() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if now.Sub(s.renewWindow) >= time.Minute {
		s.renewWindow = now
		s.renewCount = 0
	}
	s.renewCount++
	return s.renewCount <= 4
}

func (c *Client) renewNativeLease(raw []byte) bool {
	s := c.native
	if !s.allowRenew() {
		return false
	}
	token, valid := parseNativeRenewFrame(raw)
	if !valid || !c.commandsAllowed() {
		return false
	}
	generation := s.currentGeneration()
	fresh, err := s.authenticator.AuthenticateAccessLease(s.ctx, token)
	if err != nil {
		return false
	}
	if c.closed.Load() || !s.install(generation, fresh, true) {
		return false
	}
	c.SendEvent("native_access_renewed", map[string]any{"access_expires_at": fresh.Principal().AccessExpiresAt()})
	return true
}
