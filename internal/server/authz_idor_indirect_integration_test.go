//go:build integration

package server

import (
	"bytes"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestRoutesThatReachAChannelIndirectlyHideAPrivateOne covers what
// requireEveryChannelRouteProbed cannot walk: routes that name a channel
// through a message, a Talk, a conversation or a media id instead of a
// "/channels/{id}" segment. For an outsider each must answer a hidden object in
// exactly the bytes it answers an id nobody has.
func TestRoutesThatReachAChannelIndirectlyHideAPrivateOne(t *testing.T) {
	f := newAccessFixture(t, false)
	secret := f.privateChannel(t, "staff-only", "text")
	secretTalk := f.privateChannel(t, "staff-talk", "voice")
	open := f.a.createChannel(f.owner, "open", "text")
	openTalk := f.a.createChannel(f.owner, "open-talk", "voice")

	first := f.owner.post("/api/channels/"+secret.String()+"/messages", map[string]any{"content": "classified"})
	wantStatus(t, "message", first, http.StatusCreated)
	var created struct {
		ID     uuid.UUID
		Number int64
	}
	first.decode(t, &created)
	m := created.ID.String()
	up := f.owner.upload("/api/channels/"+secret.String()+"/upload", "file", "plan.png", "image/png", pngBytes, nil)
	wantStatus(t, "upload", up, http.StatusCreated)
	var shot uploaded
	up.decode(t, &shot)
	media := shot.Attachments[0].ID.String()

	// A conversation of two others, and one of the outsider's own.
	theirs := f.owner.mustDM(t, f.insider)
	wantStatus(t, "dm message", f.owner.dmPost(theirs.ID, "between us"), http.StatusCreated)
	mine := f.outsider.mustDM(t, f.owner)
	dm := theirs.ID.String()

	probes := []channelProbe{
		{"forward into an open channel", "POST", "/api/messages/" + m + "/forward", map[string]any{"channel_id": open}},
		{"forward targets", "GET", "/api/messages/" + m + "/forward", nil},
		{"react", "POST", "/api/messages/" + m + "/reactions", map[string]string{"emoji": "👍"}},
		{"thread", "GET", "/api/messages/" + m + "/thread", nil},
		{"report", "POST", "/api/messages/" + m + "/report", map[string]any{"category": "spam"}},
		{"save", "PUT", "/api/me/saved/" + m, nil},
		{"follow", "PUT", "/api/threads/" + m + "/follow", nil},
		{"media", "GET", shot.Attachments[0].URL, nil},
		{"rename talk", "PATCH", "/api/talks/" + secretTalk.String(), map[string]any{"name": "x"}},
		{"invite to talk", "POST", "/api/talks/" + secretTalk.String() + "/invites", map[string]any{"conversation_id": mine.ID}},
		{"invite into their conversation", "POST", "/api/talks/" + openTalk.String() + "/invites", map[string]any{"conversation_id": theirs.ID}},
		{"conversation", "GET", "/api/dms/" + dm, nil},
		{"hide conversation", "PUT", "/api/dms/" + dm + "/hidden", map[string]any{"hidden": true}},
		{"add to conversation", "POST", "/api/dms/" + dm + "/members", map[string]any{"user_id": f.outsider.user.ID}},
		{"leave conversation", "DELETE", "/api/dms/" + dm + "/members/@me", nil},
		{"call from conversation", "POST", "/api/dms/" + dm + "/talk", nil},
	}
	hidden := []string{m, media, secretTalk.String(), dm}
	for _, p := range probes {
		want := http.StatusNotFound
		if p.what == "forward into an open channel" {
			want = http.StatusForbidden // documented: every refusal answers the same 403 text
		}
		p.mustLookLikeAnUnknownID(t, f.outsider, hidden, want)
	}

	// The same ids work for someone who may see them, so the refusals above
	// are about the outsider and not about broken probes.
	wantStatus(t, "insider media", f.insider.get(shot.Attachments[0].URL), http.StatusOK)
	wantStatus(t, "insider thread", f.insider.get("/api/messages/"+m+"/thread"), http.StatusOK)
	wantStatus(t, "insider conversation", f.insider.get("/api/dms/"+dm), http.StatusOK)

	// By number: the twin is a number no message has.
	byNumber := channelProbe{"by number", "GET", "/api/messages/by-number/" + strconv.FormatInt(created.Number, 10), nil}
	res, twin := byNumber.send(f.outsider, byNumber.path), byNumber.send(f.outsider, "/api/messages/by-number/987654321")
	if res.status != http.StatusNotFound || twin.status != res.status || !bytes.Equal(twin.body, res.body) {
		t.Errorf("by number: a hidden message answers %d %s, an unknown one %d %s", res.status, res.body, twin.status, twin.body)
	}
}

// mustLookLikeAnUnknownID requires the refusal want, byte for byte the answer to
// the same request with every hidden id in path and body replaced by a fresh
// one. Unlike mustLookNonexistent it also swaps ids that are not channels.
func (p channelProbe) mustLookLikeAnUnknownID(t *testing.T, c *client, hidden []string, want int) {
	t.Helper()
	res := p.send(c, p.path)
	if res.status != want {
		t.Errorf("%s %s: %d %s, want %d", p.method, p.what, res.status, res.body, want)
	}
	twin, changed := p, false
	for _, id := range hidden {
		fresh := uuid.NewString()
		if strings.Contains(twin.path, id) {
			twin.path, changed = strings.ReplaceAll(twin.path, id, fresh), true
		}
		if body, ok := twin.body.(map[string]any); ok {
			swapped := map[string]any{}
			for k, v := range body {
				if s, isID := v.(uuid.UUID); isID && s.String() == id {
					v, changed = fresh, true
				}
				swapped[k] = v
			}
			twin.body = swapped
		}
	}
	if !changed {
		t.Fatalf("%s: no hidden id to swap in %s", p.what, p.path)
	}
	if got := twin.send(c, twin.path); got.status != res.status || !bytes.Equal(got.body, res.body) {
		t.Errorf("%s %s: a hidden object answers %d %s, an unknown id %d %s", p.method, p.what, res.status, res.body, got.status, got.body)
	}
}
