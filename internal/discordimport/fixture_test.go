package discordimport

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/ssrf"
)

// sampleTemplate mimics Discord's template document: numeric ids, permission
// fields as strings, an @everyone role, a hidden staff channel and a few
// kinds that need mapping (announcement, stage, forum, thread).
//
// Permission values: Moderator 16785414 = KICK_MEMBERS (2) + BAN_MEMBERS (4) +
// MANAGE_MESSAGES (8192) + MOVE_MEMBERS (1<<24); Admin 8 = ADMINISTRATOR plus
// MANAGE_CHANNELS (16) is 24; Member 3072 = VIEW_CHANNEL + SEND_MESSAGES.
const sampleTemplate = `{
  "code": "AbCdEf123456",
  "name": "Gaming Hub",
  "description": "d",
  "serialized_source_guild": {
    "name": "Gaming Hub Source",
    "roles": [
      {"id": 0, "name": "@everyone", "permissions": "1071698660929", "color": 0, "position": 0},
      {"id": 11, "name": "Member", "permissions": "3072", "color": 255, "hoist": true, "position": 1},
      {"id": 12, "name": "Moderator", "permissions": "16785414", "color": 65280, "mentionable": true, "position": 2},
      {"id": 13, "name": "Admin", "permissions": "24", "color": 16711680, "position": 3}
    ],
    "channels": [
      {"id": 1, "type": 4, "name": "Info", "position": 0, "permission_overwrites": []},
      {"id": 2, "type": 0, "name": "rules", "position": 0, "parent_id": 1, "topic": "Read first", "rate_limit_per_user": 5,
       "permission_overwrites": [{"id": 0, "type": 0, "allow": "0", "deny": "2048"}]},
      {"id": 3, "type": 5, "name": "news", "position": 1, "parent_id": 1, "permission_overwrites": []},
      {"id": 8, "type": 0, "name": "staff", "position": 2, "parent_id": 1,
       "permission_overwrites": [
         {"id": 0, "type": 0, "allow": "0", "deny": "1024"},
         {"id": 12, "type": 0, "allow": "1024", "deny": "0"},
         {"id": 99, "type": 1, "allow": "1024", "deny": "0"}]},
      {"id": 4, "type": 4, "name": "Voice", "position": 1, "permission_overwrites": []},
      {"id": 5, "type": 2, "name": "Lobby", "position": 0, "parent_id": 4, "user_limit": 5, "permission_overwrites": []},
      {"id": 6, "type": 13, "name": "Stage", "position": 1, "parent_id": 4, "permission_overwrites": []},
      {"id": 7, "type": 15, "name": "ideas", "position": 0, "parent_id": 1, "topic": "x", "permission_overwrites": []},
      {"id": 9, "type": 11, "name": "a thread", "position": 0, "parent_id": 1, "permission_overwrites": []},
      {"id": 10, "type": 0, "name": "general", "position": 0, "rate_limit_per_user": 43200, "nsfw": true, "permission_overwrites": []}
    ]
  }
}`

// serveTemplate serves body for every request and counts them.
func serveTemplate(t *testing.T, body string) (*httptest.Server, *int) {
	t.Helper()
	hits := new(int)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, hits
}

// loopbackFetcher fetches from a test server on 127.0.0.1. It has the
// production client (same dialer, no redirects) with the one exception that
// loopback is allowed, so the test server is reachable.
func loopbackFetcher(baseURL string) *Fetcher {
	allow, _ := ssrf.ParseAllow("127.0.0.0/8")
	return &Fetcher{base: baseURL + "/", client: ssrf.NewClient(ssrf.NewDialer(&ssrf.Policy{Allow: allow}), 5*time.Second)}
}
