package ws

import (
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/sfu"
)

const (
	// typingThrottle is how often one connection's typing notice is relayed
	// per channel; clients show "… schreibt" for a few seconds after the last
	// notice.
	typingThrottle = 3 * time.Second
	// maxTypingChannels bounds the per-connection typing memory.
	maxTypingChannels = 32
	// maxSpeakingChangesPerSec caps how often one connection's speaking
	// indicator may flip; a real voice activity detector stays far below it.
	maxSpeakingChangesPerSec = 8
	// Client diagnostics are logged at most diagPerWindow times per diagWindow.
	diagPerWindow = 5
	diagWindow    = 10 * time.Second
)

// allowTyping reports whether a typing notice for chID may be relayed now.
// The throttle is per channel, so alternating between channels cannot
// bypass it.
func (c *Client) allowTyping(chID uuid.UUID, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if last, ok := c.typing[chID]; ok && now.Sub(last) < typingThrottle {
		return false
	}
	if c.typing == nil {
		c.typing = map[uuid.UUID]time.Time{}
	}
	if len(c.typing) >= maxTypingChannels {
		for id, at := range c.typing {
			if now.Sub(at) >= typingThrottle {
				delete(c.typing, id)
			}
		}
		if len(c.typing) >= maxTypingChannels {
			return false
		}
	}
	c.typing[chID] = now
	return true
}

// speakingChanged records a speaking state and reports whether it should be
// relayed: only changes count, and at most maxSpeakingChangesPerSec of them
// per second. Stopping is always relayed so nobody stays shown as speaking.
func (c *Client) speakingChanged(active bool, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if active == c.speaking {
		return false
	}
	if active {
		if now.Sub(c.speakWindow) >= time.Second {
			c.speakWindow, c.speakCount = now, 0
		}
		if c.speakCount >= maxSpeakingChangesPerSec {
			return false
		}
		c.speakCount++
	}
	c.speaking = active
	return true
}

// allowDiag rate-limits logged client diagnostics.
func (c *Client) allowDiag(now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if now.Sub(c.diagWindow) >= diagWindow {
		c.diagWindow, c.diagCount = now, 0
	}
	if c.diagCount >= diagPerWindow {
		return false
	}
	c.diagCount++
	return true
}

func (c *Client) currentVoice() *uuid.UUID {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.voiceCh
}

// setVoice records the voice room this connection is in; nil drops the SFU
// peer too. A room change resets the speaking indicator.
func (c *Client) setVoice(ch *uuid.UUID) {
	c.mu.Lock()
	c.voiceCh = ch
	c.speaking = false
	if ch == nil {
		c.sfuPeer = nil
	}
	c.mu.Unlock()
}

func (c *Client) setPeer(p *sfu.Peer) {
	c.mu.Lock()
	c.sfuPeer = p
	c.mu.Unlock()
}

func (c *Client) peer() *sfu.Peer {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sfuPeer
}
