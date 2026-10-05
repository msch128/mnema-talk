package ws

// Stats is a point-in-time count of the hub's live state (admin System tab).
type Stats struct {
	// Connections are open WebSockets (a user may have several tabs).
	Connections int `json:"connections"`
	// OnlineUsers are distinct users with at least one connection.
	OnlineUsers int `json:"online_users"`
	// VoiceRooms and VoiceParticipants are the calls as members see them
	// (including users within the reconnect grace period).
	VoiceRooms        int `json:"voice_rooms"`
	VoiceParticipants int `json:"voice_participants"`
}

// Stats counts connections, online users and voice participants.
func (h *Hub) Stats() Stats {
	h.mu.RLock()
	defer h.mu.RUnlock()
	st := Stats{Connections: len(h.clients), OnlineUsers: len(h.online)}
	for _, users := range h.voice {
		if len(users) == 0 {
			continue
		}
		st.VoiceRooms++
		st.VoiceParticipants += len(users)
	}
	return st
}
