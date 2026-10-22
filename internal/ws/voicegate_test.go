package ws

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
)

func TestVoiceGateAllowsABurstThenOnePerSecond(t *testing.T) {
	var g voiceGate
	u, other := uuid.New(), uuid.New()
	now := time.Now()
	for i := range 3 {
		if !g.take(u, now) {
			t.Fatalf("token %d refused", i)
		}
	}
	if g.take(u, now) {
		t.Fatal("fourth in the same instant accepted")
	}
	if !g.take(other, now) {
		t.Fatal("another user shares the bucket")
	}
	if g.take(u, now.Add(500*time.Millisecond)) {
		t.Fatal("half a second refilled a token")
	}
	if !g.take(u, now.Add(1100*time.Millisecond)) {
		t.Fatal("no token after a second")
	}
	// A long idle time refills to the burst, not beyond.
	later := now.Add(time.Hour)
	for i := range 3 {
		if !g.take(u, later) {
			t.Fatalf("token %d after idling refused", i)
		}
	}
	if g.take(u, later) {
		t.Fatal("more than the burst after idling")
	}
	g.forget(u)
	if !g.take(u, later) {
		t.Fatal("forgotten user starts empty")
	}
}

func TestVoiceJoinOverTheAllowanceIsRefusedWithAKick(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	c := &Client{hub: h, User: auth.User{ID: uuid.New()}, send: make(chan []byte, 64)}
	h.register(c)
	ch := uuid.New()
	for range int(voiceChurnBurst) {
		h.voiceGate.take(c.User.ID, time.Now())
	}
	for len(c.send) > 0 {
		<-c.send
	}
	c.handle("voice_join", []byte(`{"channel_id":"`+ch.String()+`"}`))
	got := sentEvents(t, c, "voice_kicked")
	if len(got) != 1 || got[0]["channel_id"] != ch.String() {
		t.Fatalf("voice_kicked = %+v", got)
	}
	if c.currentVoice() != nil {
		t.Fatal("refused join seated the user")
	}
}
