package sfu

import (
	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

// Stats is a point-in-time count of what the SFU forwards (admin System tab).
type Stats struct {
	// Rooms are voice channels with at least one media connection.
	Rooms int `json:"rooms"`
	// VideoRooms are rooms with a screen share or camera.
	VideoRooms int `json:"video_rooms"`
	// Peers are media connections (one per member in a call).
	Peers int `json:"peers"`
	// ScreenShares and Cameras count publishers of each video source.
	ScreenShares int `json:"screen_shares"`
	Cameras      int `json:"cameras"`
	// AudioTracks and VideoTracks are the published tracks by kind.
	AudioTracks int `json:"audio_tracks"`
	VideoTracks int `json:"video_tracks"`
}

// Stats counts rooms, peers and video publishers across all rooms.
func (s *SFU) Stats() Stats {
	var st Stats
	for _, r := range s.roomList() {
		st.addRoom(r.countMedia())
	}
	return st
}

// roomList returns the rooms as they are now, so counting happens without
// the registry lock.
func (s *SFU) roomList() []*Room {
	s.roomsMu.Lock()
	defer s.roomsMu.Unlock()
	rooms := make([]*Room, 0, len(s.rooms))
	for _, r := range s.rooms {
		rooms = append(rooms, r)
	}
	return rooms
}

// roomCount is one room's contribution to Stats.
type roomCount struct {
	peers       int
	media       map[uuid.UUID]MediaState
	audioTracks int
	videoTracks int
}

func (r *Room) countMedia() roomCount {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c := roomCount{peers: len(r.peers), media: r.mediaStatesLocked()}
	for _, t := range r.trackLocals {
		if t.Kind == webrtc.RTPCodecTypeAudio {
			c.audioTracks++
		} else {
			c.videoTracks++
		}
	}
	return c
}

// addRoom adds one room's counts. Tracks always count; a room without peers
// does not count as a room.
func (st *Stats) addRoom(c roomCount) {
	st.AudioTracks += c.audioTracks
	st.VideoTracks += c.videoTracks
	if c.peers == 0 {
		return
	}
	st.Rooms++
	if len(c.media) > 0 {
		st.VideoRooms++
	}
	st.Peers += c.peers
	for _, m := range c.media {
		if m.Screen {
			st.ScreenShares++
		}
		if m.Camera {
			st.Cameras++
		}
	}
}
