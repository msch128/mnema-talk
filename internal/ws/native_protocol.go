package ws

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"
)

// Only the trusted native broker constructs this control. Never forward its
// payload to application handlers, diagnostics, event broadcasts or a renderer.
func parseNativeRenewFrame(raw []byte) (string, bool) {
	if len(raw) > 1024 {
		return "", false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if t, err := d.Token(); err != nil || t != json.Delim('{') {
		return "", false
	}
	seenType, seenPayload := false, false
	var token string
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return "", false
		}
		switch key {
		case "type":
			if seenType {
				return "", false
			}
			seenType = true
			t, err := d.Token()
			if err != nil || t != "native_access_renew" {
				return "", false
			}
		case "payload":
			if seenPayload {
				return "", false
			}
			seenPayload = true
			if t, err := d.Token(); err != nil || t != json.Delim('{') {
				return "", false
			}
			if key, err := d.Token(); err != nil || key != "access_token" {
				return "", false
			}
			t, err := d.Token()
			if err != nil {
				return "", false
			}
			value, ok := t.(string)
			if !ok || len(value) != 43 {
				return "", false
			}
			token = value
			if d.More() {
				return "", false
			}
			if t, err := d.Token(); err != nil || t != json.Delim('}') {
				return "", false
			}
		default:
			return "", false
		}
	}
	if t, err := d.Token(); err != nil || t != json.Delim('}') || !seenType || !seenPayload {
		return "", false
	}
	if _, err := d.Token(); err != io.EOF {
		return "", false
	}
	return token, true
}

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
	if !allowed || !valid || !c.nativeLive() {
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
