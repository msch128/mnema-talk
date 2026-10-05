package sfu

// Stats is a point-in-time count of what the SFU forwards (admin System tab).
type Stats struct {
	// Rooms are voice channels with at least one media connection.
	Rooms int `json:"rooms"`
	// Peers are media connections (one per member in a call).
	Peers int `json:"peers"`
	// ScreenShares and Cameras count publishers of each video source.
	ScreenShares int `json:"screen_shares"`
	Cameras      int `json:"cameras"`
}

// Stats counts rooms, peers and video publishers across all rooms.
func (s *SFU) Stats() Stats {
	s.roomsMu.Lock()
	rooms := make([]*Room, 0, len(s.rooms))
	for _, r := range s.rooms {
		rooms = append(rooms, r)
	}
	s.roomsMu.Unlock()

	var st Stats
	for _, r := range rooms {
		r.mu.RLock()
		peers := len(r.peers)
		media := r.mediaStatesLocked()
		r.mu.RUnlock()
		if peers == 0 {
			continue
		}
		st.Rooms++
		st.Peers += peers
		for _, m := range media {
			if m.Screen {
				st.ScreenShares++
			}
			if m.Camera {
				st.Cameras++
			}
		}
	}
	return st
}
