package ws

import (
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
)

// sendToVoiceRoom sends an event to the connections currently in chID's room.
func (h *Hub) sendToVoiceRoom(chID uuid.UUID, eventType string, payload any) {
	h.sendToVoiceRoomExcept(nil, chID, eventType, payload)
}

// sendToVoiceRoomExcept sends to every connection in chID's room except skip.
// A nil skip delivers to the whole room. The sender's other connections stay
// included: a second tab is another member of the same room.
func (h *Hub) sendToVoiceRoomExcept(skip *Client, chID uuid.UUID, eventType string, payload any) {
	data := encode(eventType, payload)
	if data == nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if c == skip {
			continue
		}
		if cur := c.currentVoice(); cur != nil && *cur == chID {
			c.deliver(data)
		}
	}
}

func (h *Hub) isMuted(userID, chID uuid.UUID) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	st := h.voiceMute[voiceKey{userID, chID}]
	return st.Muted || st.Deafened
}

// setMuteState records c's mute/deafen state and tells everyone, so the
// microphone and headphone marks show on their avatar. Deafened implies muted.
func (h *Hub) setMuteState(c *Client, st MuteState) {
	cur := c.currentVoice()
	if cur == nil {
		return
	}
	if st.Deafened {
		st.Muted = true
	}
	key := voiceKey{c.User.ID, *cur}
	h.mu.Lock()
	if !c.commandsAllowed() {
		h.mu.Unlock()
		return
	}
	if _, seated := h.voice[key.ch][key.user]; !seated {
		// A leave or kick got in between: a mark written now would outlive
		// the stay and be announced with the next one.
		h.mu.Unlock()
		return
	}
	changed := h.voiceMute[key] != st
	h.voiceMute[key] = st
	h.mu.Unlock()
	if !changed {
		return
	}
	c.emitApplicationEvent(events.VoiceMuteState, map[string]any{"channel_id": *cur, "user_id": c.User.ID, "muted": st.Muted, "deafened": st.Deafened}, false, nil, *cur)
	if st.Muted && c.speakingChanged(false, time.Now()) {
		c.emitApplicationEvent(events.VoiceSpeaking, map[string]any{"channel_id": *cur, "user_id": c.User.ID, "active": false}, false, cur, uuid.Nil)
	}
}
