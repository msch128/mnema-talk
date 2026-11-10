//go:build integration

package readstate

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/chattest"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
	"github.com/msch128/mnema-talk/internal/testutil"
)

func TestMain(m *testing.M) { os.Exit(testutil.Main(m)) }

func TestReadStateHandlerValidationAndErrors(t *testing.T) {
	p, u := chattest.NewMember(t)
	channel := chattest.NewChannel(t, p, "readstate-chat")
	message := chattest.NewMessage(t, p, channel, u.ID, "hello world")
	a := API{DB: p, Events: &events.Recorder{}, Guard: core.NewGuard(perm.NewService(p))}
	valid := map[string]string{"channelID": channel.String(), "messageID": message.String(), "id": channel.String()}
	type handler func(http.ResponseWriter, *http.Request) error

	for _, c := range []struct {
		name string
		fn   handler
	}{{"mark read", a.markRead}, {"mark unread", a.markUnread}, {"notify level", a.setNotifyLevel}} {
		t.Run(c.name+" invalid path", func(t *testing.T) {
			chattest.RequireError(t, c.fn(httptest.NewRecorder(), chattest.Request(u, "{}", nil, "")), "invalid")
		})
		t.Run(c.name+" invalid JSON", func(t *testing.T) {
			if e := c.fn(httptest.NewRecorder(), chattest.Request(u, "{", valid, "")); e == nil {
				t.Fatal("malformed JSON accepted")
			}
		})
	}
	for _, c := range []struct {
		name   string
		fn     handler
		body   string
		params map[string]string
		want   string
	}{
		{"mark read absent message", a.markRead, `{"message_id":"` + uuid.NewString() + `"}`, valid, "message not found"},
		{"mark unread absent message", a.markUnread, `{"message_id":"` + uuid.NewString() + `"}`, valid, "message not found"},
		{"invalid notification level", a.setNotifyLevel, `{"level":"unknown"}`, valid, "level"},
	} {
		t.Run(c.name, func(t *testing.T) {
			chattest.RequireError(t, c.fn(httptest.NewRecorder(), chattest.Request(u, c.body, c.params, "")), c.want)
		})
	}

	// Database cancellation must also propagate through the HTTP handler.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := chattest.Request(u, "", valid, "").WithContext(auth.WithUser(ctx, u))
	if err := a.readState(httptest.NewRecorder(), r); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}

// When the permission state cannot be loaded the guarded handlers fail closed
// instead of letting the request through, and the failure is not a refusal.
func TestReadStateHandlersFailClosedWithoutPermissionState(t *testing.T) {
	p, u := chattest.NewMember(t)
	channel := chattest.NewChannel(t, p, "readstate-closed")
	a := API{DB: p, Events: &events.Recorder{}, Guard: core.Guard{Perms: permtest.Unloadable()}}
	params := map[string]string{"channelID": channel.String()}
	for _, c := range []struct {
		name   string
		run    func(*http.Request) error
		params map[string]string
	}{
		{"read state", func(r *http.Request) error { return a.readState(httptest.NewRecorder(), r) }, nil},
		{"mark read", func(r *http.Request) error { return a.markRead(httptest.NewRecorder(), r) }, params},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := c.run(chattest.Request(u, "", c.params, ""))
			if err == nil {
				t.Fatal("the handler let the request through without a permission state")
			}
			if _, refusal := httpx.AsAPIError(err); refusal {
				t.Fatalf("a load failure must not look like a refusal: %v", err)
			}
		})
	}
}
