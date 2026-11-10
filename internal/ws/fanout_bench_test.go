package ws

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
)

// benchSendBuffer is each benchmark client's queue; the queues are emptied
// off the clock whenever they are full, so no client is ever dropped as slow.
const benchSendBuffer = 256

// benchClients registers n connections of n users on h. They are entered
// directly: a greeting per connection would cost more than the benchmark.
func benchClients(h *Hub, n int) []*Client {
	clients := make([]*Client, n)
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range clients {
		c := newClient(h, nil, auth.User{ID: uuid.New(), Username: fmt.Sprintf("member-%d", i)}, 0)
		c.send = make(chan []byte, benchSendBuffer)
		h.clients[c] = struct{}{}
		h.online[c.User.ID]++
		clients[i] = c
	}
	return clients
}

// drainEvery empties the queues of clients after every benchSendBuffer events,
// with the timer stopped.
func drainEvery(b *testing.B, i int, clients []*Client) {
	if (i+1)%benchSendBuffer != 0 {
		return
	}
	b.StopTimer()
	for _, c := range clients {
		for len(c.send) > 0 {
			<-c.send
		}
	}
	b.StartTimer()
}

// One event of a Talk with 20 members (a speaking indicator), while the
// server holds 20, 1,000 and 5,000 connections in total.
func BenchmarkVoiceRoomEvent(b *testing.B) {
	const members = 20
	for _, total := range []int{20, 1000, 5000} {
		b.Run(fmt.Sprintf("room=%d/clients=%d", members, total), func(b *testing.B) {
			h := NewHub(nil, nil, nil, nil, openPerms())
			clients := benchClients(h, total)
			room := uuid.New()
			for _, c := range clients[:members] {
				c.setVoice(&room)
			}
			payload := map[string]any{"channel_id": room, "user_id": clients[0].User.ID, "active": true}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				h.sendToVoiceRoom(room, events.VoiceSpeaking, payload)
				drainEvery(b, i, clients[:members])
			}
		})
	}
}

// One event of a text channel that every member may view (a new message),
// with 1,000 and 5,000 connections.
func BenchmarkBroadcastChannel(b *testing.B) {
	for _, total := range []int{1000, 5000} {
		b.Run(fmt.Sprintf("clients=%d", total), func(b *testing.B) {
			h := NewHub(nil, nil, nil, nil, openPerms())
			clients := benchClients(h, total)
			channel := openChannel()
			payload := map[string]any{"channel_id": channel, "id": uuid.New(), "content": "hello"}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				h.BroadcastChannel(channel, events.MessageCreate, payload)
				drainEvery(b, i, clients)
			}
		})
	}
}
