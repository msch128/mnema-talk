package push

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func heldFor(user, channel uuid.UUID, mention bool, text string) held {
	return held{user: user, notice: Notice{ChannelID: channel, Mention: mention, Content: text, Count: 1}}
}

func TestBurstInOneChannelBecomesOneLaterPush(t *testing.T) {
	clk := &clock{t: time.Unix(1_000, 0)}
	c := newCoalescer(40*time.Millisecond, clk.now)
	user, ch := uuid.New(), uuid.New()
	flushed := make(chan held, 4)
	flush := func(h held) { flushed <- h }
	k := key{user, ch}

	if first := c.add(k, heldFor(user, ch, false, "one"), flush); first == nil || first.notice.Count != 1 {
		t.Fatalf("the first message must go out at once, got %+v", first)
	}
	clk.advance(10 * time.Millisecond)
	if c.add(k, heldFor(user, ch, false, "two"), flush) != nil || c.add(k, heldFor(user, ch, false, "three"), flush) != nil {
		t.Fatal("messages inside the window must wait")
	}
	select {
	case h := <-flushed:
		if h.notice.Count != 2 || h.notice.Content != "three" {
			t.Fatalf("held push %+v: want two messages, the latest text", h.notice)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the held push never came")
	}
}

func TestAMentionIsNeverHeldAndTakesTheWaitingMessagesWithIt(t *testing.T) {
	clk := &clock{t: time.Unix(1_000, 0)}
	c := newCoalescer(time.Hour, clk.now)
	user, ch := uuid.New(), uuid.New()
	flush := func(held) { t.Fatal("a merged push must not be flushed again") }
	k := key{user, ch}

	c.add(k, heldFor(user, ch, false, "one"), flush)
	clk.advance(time.Second)
	c.add(k, heldFor(user, ch, false, "two"), flush)
	got := c.add(k, heldFor(user, ch, true, "@you"), flush)
	if got == nil || got.notice.Count != 2 || !got.notice.Mention || got.notice.Content != "@you" {
		t.Fatalf("%+v", got)
	}
}

func TestAfterTheWindowTheNextMessageGoesOutAtOnce(t *testing.T) {
	clk := &clock{t: time.Unix(1_000, 0)}
	c := newCoalescer(time.Minute, clk.now)
	user, ch := uuid.New(), uuid.New()
	k := key{user, ch}
	c.add(k, heldFor(user, ch, false, "one"), func(held) {})
	clk.advance(2 * time.Minute)
	if c.add(k, heldFor(user, ch, false, "two"), func(held) {}) == nil {
		t.Fatal("a message after the window was held")
	}
}

func TestChannelsAndMembersDoNotShareAWindow(t *testing.T) {
	clk := &clock{t: time.Unix(1_000, 0)}
	c := newCoalescer(time.Hour, clk.now)
	a, b, ch1, ch2 := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, k := range []key{{a, ch1}, {a, ch2}, {b, ch1}} {
		if c.add(k, heldFor(k.user, k.channel, false, "x"), func(held) {}) == nil {
			t.Fatalf("%+v was held by another key's window", k)
		}
	}
}

func TestSweepForgetsQuietChannelsButKeepsWaitingOnes(t *testing.T) {
	clk := &clock{t: time.Unix(1_000, 0)}
	c := newCoalescer(time.Minute, clk.now)
	user, ch1, ch2 := uuid.New(), uuid.New(), uuid.New()
	c.add(key{user, ch1}, heldFor(user, ch1, false, "x"), func(held) {})
	c.add(key{user, ch2}, heldFor(user, ch2, false, "x"), func(held) {})
	c.add(key{user, ch2}, heldFor(user, ch2, false, "y"), func(held) {}) // waits
	clk.advance(10 * time.Minute)
	c.sweep()
	if _, ok := c.entries[key{user, ch1}]; ok {
		t.Fatal("a quiet channel is remembered")
	}
	if _, ok := c.entries[key{user, ch2}]; !ok {
		t.Fatal("a waiting push was forgotten")
	}
	c.stop()
}
