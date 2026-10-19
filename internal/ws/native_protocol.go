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
	s := c.native
	s.mu.Lock()
	now := time.Now()
	if now.Sub(s.renewWindow) >= time.Minute {
		s.renewWindow = now
		s.renewCount = 0
	}
	s.renewCount++
	allowed := s.renewCount <= 4
	s.mu.Unlock()
	token, valid := parseNativeRenewFrame(raw)
	if !allowed || !valid || !c.commandsAllowed() {
		c.hub.terminateNativeClient(c)
		return true
	}
	generation, _, _ := s.snapshot()
	fresh, err := s.authenticator.AuthenticateAccessLease(s.ctx, token)
	if err != nil || c.closed.Load() || !s.install(generation, fresh, true) {
		c.hub.terminateNativeClient(c)
		return true
	}
	c.SendEvent("native_access_renewed", map[string]any{"access_expires_at": fresh.Principal().AccessExpiresAt()})
	return true
}
