//go:build integration

package later

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/httpx"
)

func scheduleVia(t *testing.T, f fixture, s *Service, channel uuid.UUID, body map[string]any) error {
	t.Helper()
	user, err := auth.GetUser(context.Background(), f.p, f.user)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(body)
	rc := chi.NewRouteContext()
	rc.URLParams.Add("channelID", channel.String())
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(raw)))
	r.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(auth.WithUser(r.Context(), user), chi.RouteCtxKey, rc)
	return s.createScheduled(httptest.NewRecorder(), r.WithContext(ctx))
}

func TestATopLevelMessageCannotBeScheduledIntoAForumChannel(t *testing.T) {
	f := newFixture(t)
	s := f.service(f.p)
	forum, err := chat.CreateChannel(context.Background(), f.p, nil, "ideas", chat.ChannelTypeForum, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"content": "hello", "send_at": time.Now().Add(time.Hour)}
	err = scheduleVia(t, f, s, forum.ID, body)
	var apiErr *httpx.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest {
		t.Fatalf("scheduling a top-level forum message: %v, want a 400", err)
	}
	if err := scheduleVia(t, f, s, f.channel, body); err != nil {
		t.Errorf("a text channel accepts it: %v", err)
	}
}

func TestADatabaseFailureWhileCheckingTheTargetIsNotARefusal(t *testing.T) {
	f := newFixture(t)
	closed := closedPool(t, f.p)
	parent := uuid.New()
	err := chat.ValidateTarget(context.Background(), closed, f.channel, &parent, nil)
	var apiErr *httpx.APIError
	if err == nil || errors.As(err, &apiErr) {
		t.Fatalf("parent check on a dead database: %v, want a plain error", err)
	}
	err = chat.ValidateTarget(context.Background(), closed, f.channel, nil, &parent)
	if err == nil || errors.As(err, &apiErr) {
		t.Fatalf("reply check on a dead database: %v, want a plain error", err)
	}
	// A message that does not exist stays a refusal.
	err = chat.ValidateTarget(context.Background(), f.p, f.channel, &parent, nil)
	if !errors.As(err, &apiErr) {
		t.Errorf("unknown parent: %v, want an API error", err)
	}
}

func TestAnAccountLoadedForSendingIsActiveAndNotABot(t *testing.T) {
	f := newFixture(t)
	s := f.service(f.p)
	ctx := context.Background()
	if u, ok, err := s.activeUser(ctx, f.user); err != nil || !ok || u.ID != f.user {
		t.Fatalf("activeUser = %v %v %v", u, ok, err)
	}
	if _, err := f.p.Exec(ctx, `UPDATE users SET disabled_at = now() WHERE id = $1`, f.user); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.activeUser(ctx, f.user); err != nil || ok {
		t.Errorf("a disabled account: ok=%v err=%v", ok, err)
	}
}
