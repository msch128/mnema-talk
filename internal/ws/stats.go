package ws

// Stats is a point-in-time count of the hub's live state (admin System tab).
type Stats struct {
	// Connections are open WebSockets (a user may have several tabs).
	Connections int `json:"connections"`
	// OnlineUsers are distinct users with at least one connection.
	OnlineUsers int `json:"online_users"`
	// Sessions are distinct client sessions among open WebSockets.
	Sessions int `json:"sessions"`
	// VoiceRooms and VoiceParticipants are the calls as members see them
	// (including users within the reconnect grace period).
	VoiceRooms        int `json:"voice_rooms"`
	VoiceParticipants int `json:"voice_participants"`
}

// Stats counts connections, online users, active sessions and voice participants.
func (h *Hub) Stats() Stats {
	h.mu.RLock()
	defer h.mu.RUnlock()
	st := Stats{Connections: len(h.clients), OnlineUsers: len(h.online)}
	sessions := make(map[string]struct{})
	for c := range h.clients {
		if c.sessionID != "" {
			sessions["web:"+c.sessionID] = struct{}{}
		} else {
			sessions["user:"+c.User.ID.String()] = struct{}{}
		}
	}
	st.Sessions = len(sessions)
	for _, users := range h.voice {
		if len(users) == 0 {
			continue
		}
		st.VoiceRooms++
		st.VoiceParticipants += len(users)
	}
	return st
}
