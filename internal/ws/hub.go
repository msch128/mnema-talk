package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/sfu"
	"github.com/pion/webrtc/v4"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow cross-origin WebSocket connections (handled by Reverse Proxy)
	},
}

type Event struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

type Client struct {
	hub            *Hub
	conn           *websocket.Conn
	send           chan []byte
	User           auth.User
	CurrentVoiceCh *uuid.UUID
}

type Hub struct {
	clients    map[*Client]bool
	clientsMu  sync.RWMutex
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client

	// Voice presence: channel_id -> list of users in voice
	voicePresence   map[uuid.UUID]map[uuid.UUID]auth.User
	voicePresenceMu sync.RWMutex

	// Online presence: user_id -> connection count
	onlineUsers   map[uuid.UUID]int
	onlineUsersMu sync.RWMutex

	SFU *sfu.SFU
}

func NewHub(voiceSFU *sfu.SFU) *Hub {
	return &Hub{
		clients:       make(map[*Client]bool),
		broadcast:     make(chan []byte, 256),
		register:      make(chan *Client),
		unregister:    make(chan *Client),
		voicePresence: make(map[uuid.UUID]map[uuid.UUID]auth.User),
		onlineUsers:   make(map[uuid.UUID]int),
		SFU:           voiceSFU,
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.clientsMu.Lock()
			h.clients[client] = true
			h.clientsMu.Unlock()
			log.Printf("[WS] User connected: %s (%s)\n", client.User.DisplayName, client.User.ID)

			// 1. Update online presence
			h.onlineUsersMu.Lock()
			isFirst := h.onlineUsers[client.User.ID] == 0
			h.onlineUsers[client.User.ID]++
			h.onlineUsersMu.Unlock()

			if isFirst {
				h.BroadcastEvent("presence_update", map[string]interface{}{
					"user_id": client.User.ID,
					"status":  "online",
				})
			}

			// 2. Send initial snapshots
			h.sendPresenceSnapshot(client)
			h.sendVoiceStateSnapshot(client)

		case client := <-h.unregister:
			h.clientsMu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.clientsMu.Unlock()

			// 1. Update online presence
			h.onlineUsersMu.Lock()
			h.onlineUsers[client.User.ID]--
			isOffline := h.onlineUsers[client.User.ID] <= 0
			if isOffline {
				delete(h.onlineUsers, client.User.ID)
			}
			h.onlineUsersMu.Unlock()

			if isOffline {
				h.BroadcastEvent("presence_update", map[string]interface{}{
					"user_id": client.User.ID,
					"status":  "offline",
				})
			}

			// 2. If client was in a voice channel, remove them and notify everyone
			if client.CurrentVoiceCh != nil {
				h.LeaveVoice(client, *client.CurrentVoiceCh)
			}
			log.Printf("[WS] User disconnected: %s\n", client.User.DisplayName)

		case message := <-h.broadcast:
			h.clientsMu.RLock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.clientsMu.RUnlock()
		}
	}
}

// BroadcastEvent encodes and broadcasts a typed event to all connected clients
func (h *Hub) BroadcastEvent(eventType string, payload interface{}) {
	data, err := json.Marshal(Event{Type: eventType, Payload: payload})
	if err == nil {
		h.broadcast <- data
	}
}

func (h *Hub) JoinVoice(client *Client, channelID uuid.UUID) {
	h.voicePresenceMu.Lock()
	if _, ok := h.voicePresence[channelID]; !ok {
		h.voicePresence[channelID] = make(map[uuid.UUID]auth.User)
	}
	h.voicePresence[channelID][client.User.ID] = client.User
	client.CurrentVoiceCh = &channelID
	h.voicePresenceMu.Unlock()

	if h.SFU != nil {
		room := h.SFU.GetOrCreateRoom(channelID)
		_, err := room.JoinPeer(client.User.ID, func(offer webrtc.SessionDescription) {
			client.SendEvent("webrtc_offer", offer)
		}, func(candidate *webrtc.ICECandidateInit) {
			client.SendEvent("webrtc_candidate", candidate)
		})
		if err != nil {
			log.Printf("[SFU] Error joining peer to SFU room: %v", err)
		}
	}

	h.BroadcastEvent("voice_state_update", map[string]interface{}{
		"action":     "join",
		"channel_id": channelID,
		"user":       client.User,
	})
}

func (h *Hub) LeaveVoice(client *Client, channelID uuid.UUID) {
	if h.SFU != nil {
		room := h.SFU.GetOrCreateRoom(channelID)
		room.RemovePeer(client.User.ID)
	}

	h.voicePresenceMu.Lock()
	if chUsers, ok := h.voicePresence[channelID]; ok {
		delete(chUsers, client.User.ID)
		if len(chUsers) == 0 {
			delete(h.voicePresence, channelID)
		}
	}
	client.CurrentVoiceCh = nil
	h.voicePresenceMu.Unlock()

	h.BroadcastEvent("voice_state_update", map[string]interface{}{
		"action":     "leave",
		"channel_id": channelID,
		"user_id":    client.User.ID,
	})
}

func (h *Hub) sendVoiceStateSnapshot(client *Client) {
	h.voicePresenceMu.RLock()
	defer h.voicePresenceMu.RUnlock()

	client.SendEvent("voice_snapshot", h.voicePresence)
}

func (h *Hub) sendPresenceSnapshot(client *Client) {
	h.onlineUsersMu.RLock()
	defer h.onlineUsersMu.RUnlock()

	onlineList := make([]uuid.UUID, 0, len(h.onlineUsers))
	for userID := range h.onlineUsers {
		onlineList = append(onlineList, userID)
	}
	client.SendEvent("presence_snapshot", onlineList)
}

func (c *Client) SendEvent(eventType string, payload interface{}) {
	data, err := json.Marshal(Event{Type: eventType, Payload: payload})
	if err == nil {
		select {
		case c.send <- data:
		default:
		}
	}
}

// HandleWebSocket upgrades HTTP connection and registers client
func (h *Hub) HandleWebSocket(jwtSecret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tokenStr := r.URL.Query().Get("token")
		if tokenStr == "" {
			if cookie, err := r.Cookie("auth_token"); err == nil {
				tokenStr = cookie.Value
			}
		}

		if tokenStr == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		claims, err := auth.ValidateToken(tokenStr, jwtSecret)
		if err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("[WS] Upgrade failed: %v\n", err)
			return
		}

		client := &Client{
			hub:  h,
			conn: conn,
			send: make(chan []byte, 256),
			User: auth.User{
				ID:          claims.UserID,
				Username:    claims.Username,
				DisplayName: claims.Username,
				Role:        claims.Role,
			},
		}

		h.register <- client

		go client.writePump()
		go client.readPump()
	}
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(512 * 1024)
	_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			break
		}

		var event struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}

		if err := json.Unmarshal(message, &event); err != nil {
			continue
		}

		switch event.Type {
		case "voice_join":
			var payload struct {
				ChannelID uuid.UUID `json:"channel_id"`
			}
			if err := json.Unmarshal(event.Payload, &payload); err == nil {
				if c.CurrentVoiceCh != nil && *c.CurrentVoiceCh != payload.ChannelID {
					c.hub.LeaveVoice(c, *c.CurrentVoiceCh)
				}
				c.hub.JoinVoice(c, payload.ChannelID)
			}

		case "voice_leave":
			if c.CurrentVoiceCh != nil {
				c.hub.LeaveVoice(c, *c.CurrentVoiceCh)
			}

		case "voice_speaking":
			var payload struct {
				Active bool `json:"active"`
			}
			if err := json.Unmarshal(event.Payload, &payload); err == nil && c.CurrentVoiceCh != nil {
				c.hub.BroadcastEvent("voice_speaking", map[string]interface{}{
					"channel_id": *c.CurrentVoiceCh,
					"user_id":    c.User.ID,
					"active":     payload.Active,
				})
			}

		case "ping":
			var payload struct {
				T float64 `json:"t"`
			}
			if err := json.Unmarshal(event.Payload, &payload); err == nil {
				c.SendEvent("pong", map[string]interface{}{
					"t": payload.T,
				})
			}

		case "webrtc_answer":
			var answer webrtc.SessionDescription
			if err := json.Unmarshal(event.Payload, &answer); err == nil && c.CurrentVoiceCh != nil && c.hub.SFU != nil {
				room := c.hub.SFU.GetOrCreateRoom(*c.CurrentVoiceCh)
				peer := room.GetPeer(c.User.ID)
				if peer != nil {
					if err := peer.PC.SetRemoteDescription(answer); err != nil {
						log.Printf("[SFU] Error setting remote description for %s: %v", c.User.ID, err)
					}
				}
			}

		case "webrtc_candidate":
			var candidate webrtc.ICECandidateInit
			if err := json.Unmarshal(event.Payload, &candidate); err == nil && c.CurrentVoiceCh != nil && c.hub.SFU != nil {
				room := c.hub.SFU.GetOrCreateRoom(*c.CurrentVoiceCh)
				peer := room.GetPeer(c.User.ID)
				if peer != nil {
					if err := peer.PC.AddICECandidate(candidate); err != nil {
						log.Printf("[SFU] Error adding ICE candidate for %s: %v", c.User.ID, err)
					}
				}
			}

		case "webrtc_request_keyframe":
			if c.CurrentVoiceCh != nil && c.hub.SFU != nil {
				room := c.hub.SFU.GetOrCreateRoom(*c.CurrentVoiceCh)
				room.DispatchKeyframe(c.User.ID)
			}
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			_, _ = w.Write(message)

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
