//go:build integration

package server

import (
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"
)

// GET /api/read-state says per channel the number of the newest message its
// counts include (last_message_number), and a client relies on the counts
// holding exactly the messages up to that number: then it can tell, for a
// message_create that crosses the answer, whether the answer already counts
// it. Several members write into one channel at once while another reads the
// read state; afterwards every answer must count exactly the messages whose
// number is at most its mark.
func TestTheReadStateCountsExactlyTheMessagesUpToItsMark(t *testing.T) {
	a := newApp(t, true)
	owner := a.seedAdmin()
	ch := a.createChannel(owner, "busy", "text")
	reader := a.register(owner, "max")
	writers := []*client{owner, a.register(owner, "alice"), a.register(owner, "bob")}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for w, c := range writers {
		wg.Go(func() {
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				if res := c.post("/api/channels/"+ch.String()+"/messages", map[string]any{"content": "busy " + strconv.Itoa(w) + " " + strconv.Itoa(i)}); res.status != http.StatusCreated {
					t.Errorf("post: %d %s", res.status, res.body)
					return
				}
				time.Sleep(150 * time.Millisecond) // under the send rate limit for the whole run
			}
		})
	}
	var answers []readState
	for range 40 {
		answers = append(answers, reader.readState(t, ch))
		time.Sleep(25 * time.Millisecond)
	}
	close(stop)
	wg.Wait()

	res := reader.get("/api/channels/" + ch.String() + "/messages?limit=100")
	if res.status != http.StatusOK {
		t.Fatalf("history: %d %s", res.status, res.body)
	}
	var history []struct {
		Number int64 `json:"number"`
	}
	res.decode(t, &history)
	if len(history) < 10 || len(history) >= 100 {
		t.Fatalf("%d messages: the run measured nothing or the page is too small", len(history))
	}
	for i, s := range answers {
		want := 0
		for _, m := range history {
			if m.Number <= s.LastMessageNumber {
				want++
			}
		}
		if s.UnreadCount != want {
			t.Errorf("answer %d: unread %d, but %d messages are numbered up to its mark %d", i, s.UnreadCount, want, s.LastMessageNumber)
		}
	}
}
