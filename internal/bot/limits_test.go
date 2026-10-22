package bot

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
)

// Every spelling of one channel UUID shares the bot's send bucket, so rotating
// the spelling cannot multiply the per-channel limit.
func TestSendLimitIsPerCanonicalChannel(t *testing.T) {
	a := NewAuthenticator(nil)
	a.SetLimits(1_000_000, 3)
	ch := uuid.New()
	spellings := []string{
		ch.String(),
		strings.ToUpper(ch.String()),
		"{" + ch.String() + "}",
		"urn:uuid:" + ch.String(),
		strings.ReplaceAll(ch.String(), "-", ""),
		strings.ToUpper(strings.ReplaceAll(ch.String(), "-", "")),
	}
	r := chi.NewRouter()
	r.With(a.LimitSends(ChannelParam)).Post("/channels/{channelID}/messages", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	b := &auth.BotInfo{BotID: uuid.New()}
	codes := []int{}
	for _, s := range spellings {
		req := httptest.NewRequest(http.MethodPost, "/channels/"+s+"/messages", nil)
		req = req.WithContext(auth.WithBot(req.Context(), b))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		codes = append(codes, rec.Code)
	}
	want := []int{204, 204, 204, 429, 429, 429}
	for i := range want {
		if codes[i] != want[i] {
			t.Fatalf("responses %v, want %v", codes, want)
		}
	}
	// A different channel keeps its own budget; a malformed ID is left to the
	// handler to refuse.
	for _, path := range []string{"/channels/" + uuid.NewString() + "/messages", "/channels/not-a-uuid/messages"} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req = req.WithContext(auth.WithBot(req.Context(), b))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s: %d", path, rec.Code)
		}
	}
}

func TestInteractionStoreCapsPerBotAndPerInvoker(t *testing.T) {
	now := time.Now()
	s := &interactionStore{m: map[uuid.UUID]*interaction{}, now: func() time.Time { return now }}
	botA, botB := uuid.New(), uuid.New()
	add := func(bot, invoker uuid.UUID) bool {
		_, ok := s.add(uuid.New(), &interaction{botID: bot, invoker: invoker})
		return ok
	}

	// One account stops at its cap, others still get through.
	spammer := uuid.New()
	for i := 0; i < maxPendingPerInvoker; i++ {
		if !add(botA, spammer) {
			t.Fatalf("run %d refused below the per-account cap", i)
		}
	}
	if add(botA, spammer) || add(botB, spammer) {
		t.Fatal("an account past its cap must be refused, whatever the bot")
	}
	if !add(botA, uuid.New()) {
		t.Fatal("another member was starved by one account")
	}

	// Many accounts cannot fill the store with one bot's runs: other bots stay usable.
	for s.countBot(botA) < maxPendingPerBot {
		if !add(botA, uuid.New()) {
			t.Fatal("refused below the per-bot cap")
		}
	}
	if add(botA, uuid.New()) {
		t.Fatal("bot past its cap accepted")
	}
	if !add(botB, uuid.New()) {
		t.Fatal("commands of another bot were starved")
	}

	// Expired runs free their room.
	now = now.Add(interactionTTL + time.Second)
	if !add(botA, spammer) {
		t.Fatal("expired entries still count")
	}
	if len(s.m) != 1 {
		t.Fatalf("expired entries were not purged: %d left", len(s.m))
	}
}

func (s *interactionStore) countBot(id uuid.UUID) int {
	n := 0
	for _, v := range s.m {
		if v.botID == id {
			n++
		}
	}
	return n
}
