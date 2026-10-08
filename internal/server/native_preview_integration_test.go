//go:build integration

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/sfu"
	"github.com/msch128/mnema-talk/internal/testutil"
)

const nativePreviewFixturePassword = "native-preview-fixture-password"

type previewTestApp struct {
	server      *httptest.Server
	router      *NativePreviewRouter
	pool        *db.Pool
	user, admin auth.User
}
type previewTestGrant struct {
	User         auth.User `json:"user"`
	Access       string    `json:"access_token"`
	Refresh      string    `json:"refresh_token"`
	Family       uuid.UUID `json:"family_id"`
	Instance     uuid.UUID `json:"client_instance_id"`
	Sequence     uint64    `json:"refresh_sequence"`
	AccessExpiry time.Time `json:"access_expires_at"`
	FamilyExpiry time.Time `json:"family_expires_at"`
}

func newPreviewTestApp(t *testing.T, status NativeCompatibility, withSFU bool) *previewTestApp {
	t.Helper()
	pool := testutil.DB(t)
	testutil.Reset(t, pool)
	server := httptest.NewUnstartedServer(nil)
	origin := "https://" + server.Listener.Addr().String()
	cfg, err := config.FromEnv(func(key string) (string, bool) {
		v, ok := map[string]string{"DATABASE_URL": "unused", "PUBLIC_URL": origin, "JWT_SECRET": "native-preview-fixture-secret-native-preview", "MEDIA_RETENTION_DAYS": "0"}[key]
		return v, ok
	})
	if err != nil {
		t.Fatal("preview fixture config failed")
	}
	deps := Deps{Config: cfg, DB: pool, Version: "preview-fixture"}
	if withSFU {
		deps.SFU, err = sfu.NewSFU(0, 0, []string{"127.0.0.1"}, nil)
		if err != nil {
			t.Fatal("preview fixture SFU failed")
		}
		t.Cleanup(func() { _ = deps.SFU.Close() })
	}
	router, err := NewNativePreviewRouter(deps, NativePreviewOptions{CommunityID: "native-preview-fixture", InstanceOrigin: origin, Compatibility: status})
	if err != nil {
		t.Fatal("preview router construction failed")
	}
	server.Config.Handler = router
	server.StartTLS()
	t.Cleanup(server.Close)
	t.Cleanup(router.Close)
	a := &previewTestApp{server: server, router: router, pool: pool}
	if server.Client().Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify {
		t.Fatal("TLS fixture disabled validation")
	}
	seed := func(name string, role string) auth.User {
		user := auth.User{ID: uuid.New(), Username: name, Role: role}
		hash, err := auth.HashPassword(nativePreviewFixturePassword)
		if err != nil {
			t.Fatal("fixture password hash failed")
		}
		if _, err = pool.Exec(context.Background(), `INSERT INTO users(id,username,display_name,password_hash,role)VALUES($1,$2,$2,$3,$4)`, user.ID, name, hash, role); err != nil {
			t.Fatal("fixture account insert failed")
		}
		return user
	}
	a.user = seed("preview-user", "user")
	a.admin = seed("preview-admin", "admin")
	return a
}
func (a *previewTestApp) request(t *testing.T, method, path string, body any, access string, headers http.Header, want int) []byte {
	t.Helper()
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal("fixture body encoding failed")
		}
	}
	request, err := http.NewRequest(method, a.server.URL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal("fixture request failed")
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if access != "" {
		request.Header.Set("Authorization", "Bearer "+access)
	}
	for key, values := range headers {
		request.Header[key] = values
	}
	response, err := a.server.Client().Do(request)
	if err != nil {
		t.Fatal("fixture HTTPS request failed")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != want {
		t.Fatalf("native fixture status %d expected %d", response.StatusCode, want)
	}
	if strings.HasPrefix(path, NativePreviewPrefix) || strings.HasPrefix(path, "/.well-known/") {
		if response.Header.Get("Cache-Control") != "no-store" || len(response.Cookies()) != 0 {
			t.Fatal("native response cache/cookie boundary failed")
		}
	}
	return data
}
func (a *previewTestApp) login(t *testing.T, user auth.User) previewTestGrant {
	id := uuid.New()
	raw := a.request(t, http.MethodPost, NativePreviewPrefix+"/auth/login", map[string]string{"username": user.Username, "password": nativePreviewFixturePassword, "client_instance_id": id.String()}, "", nil, http.StatusOK)
	var grant previewTestGrant
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &grant) != nil || json.Unmarshal(raw, &fields) != nil || len(fields) != 8 || grant.User.ID != user.ID || grant.Instance != id || grant.Family == uuid.Nil || grant.Sequence != 0 || len(grant.Access) != 43 || len(grant.Refresh) != 43 {
		t.Fatal("native eight-field grant invalid")
	}
	return grant
}
func (a *previewTestApp) dial(t *testing.T, grant previewTestGrant) *websocket.Conn {
	t.Helper()
	d := *websocket.DefaultDialer
	d.TLSClientConfig = a.server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	c, response, err := d.Dial("wss"+strings.TrimPrefix(a.server.URL, "https")+NativePreviewPrefix+"/ws", http.Header{"Authorization": []string{"Bearer " + grant.Access}})
	if err != nil || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("native preview WSS failed")
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}
func previewReadUntil(t *testing.T, c *websocket.Conn, want string, forbidden map[string]bool) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		var event struct {
			Type string `json:"type"`
		}
		if c.ReadJSON(&event) != nil {
			t.Fatal("expected native metadata event absent")
		}
		if forbidden[event.Type] {
			t.Fatal("protected event reached native metadata transport")
		}
		if event.Type == want {
			return
		}
	}
}
func previewExpectClosed(t *testing.T, c *websocket.Conn) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, _, err := c.ReadMessage()
		if err == nil {
			continue
		}
		if timeout, ok := err.(interface{ Timeout() bool }); ok && timeout.Timeout() {
			t.Fatal("forbidden metadata socket remained open")
		}
		return
	}
}

func TestNativePreviewRealHTTPSMetadataAndCurrentRoles(t *testing.T) {
	a := newPreviewTestApp(t, NativeSupported, false)
	grant := a.login(t, a.user)
	admin := a.login(t, a.admin)
	for _, path := range []string{"/auth/me", "/members", "/channels", "/read-state", "/users/" + a.user.ID.String(), "/legal", "/health"} {
		a.request(t, http.MethodGet, NativePreviewPrefix+path, nil, grant.Access, nil, http.StatusOK)
	}
	a.request(t, http.MethodPut, NativePreviewPrefix+"/users/me/profile", map[string]string{"display_name": "Native preview", "bio": "synthetic metadata"}, grant.Access, nil, http.StatusOK)
	a.request(t, http.MethodPut, NativePreviewPrefix+"/users/me/locale", map[string]string{"locale": "de"}, grant.Access, nil, http.StatusOK)
	a.request(t, http.MethodPut, NativePreviewPrefix+"/users/me/presence", map[string]string{"presence": "dnd"}, grant.Access, nil, http.StatusOK)
	a.request(t, http.MethodPut, NativePreviewPrefix+"/users/me/status", map[string]string{"status_text": "preview"}, grant.Access, nil, http.StatusOK)
	for _, path := range []string{"/admin/users", "/admin/invites", "/admin/system", "/admin/system/update"} {
		a.request(t, http.MethodGet, NativePreviewPrefix+path, nil, grant.Access, nil, http.StatusForbidden)
		a.request(t, http.MethodGet, NativePreviewPrefix+path, nil, admin.Access, nil, http.StatusOK)
	}
	category := a.request(t, http.MethodPost, NativePreviewPrefix+"/admin/categories", map[string]string{"name": "native category"}, admin.Access, nil, http.StatusCreated)
	var cat struct {
		ID uuid.UUID `json:"id"`
	}
	if json.Unmarshal(category, &cat) != nil || cat.ID == uuid.Nil {
		t.Fatal("native category handler failed")
	}
	a.request(t, http.MethodPatch, NativePreviewPrefix+"/admin/categories/"+cat.ID.String(), map[string]string{"name": "renamed native"}, admin.Access, nil, http.StatusOK)
	channel := a.request(t, http.MethodPost, NativePreviewPrefix+"/admin/channels", map[string]any{"name": "native-channel", "type": "text", "category_id": cat.ID}, admin.Access, nil, http.StatusCreated)
	var ch struct {
		ID uuid.UUID `json:"id"`
	}
	if json.Unmarshal(channel, &ch) != nil || ch.ID == uuid.Nil {
		t.Fatal("native channel handler failed")
	}
	a.request(t, http.MethodPatch, NativePreviewPrefix+"/admin/channels/"+ch.ID.String(), map[string]string{"topic": "native metadata"}, admin.Access, nil, http.StatusOK)
	// Tokens contain no role snapshot: the real current users.role governs this request.
	if _, err := a.pool.Exec(context.Background(), `UPDATE users SET role='user' WHERE id=$1`, a.admin.ID); err != nil {
		t.Fatal("fixture role downgrade failed")
	}
	a.request(t, http.MethodGet, NativePreviewPrefix+"/admin/users", nil, admin.Access, nil, http.StatusForbidden)
}

func TestNativePreviewDiscoveryVersionStatesAndNoContentGrant(t *testing.T) {
	for _, state := range []NativeCompatibility{NativeSupported, NativeDeprecated, NativeUnsupported} {
		t.Run(string(state), func(t *testing.T) {
			a := newPreviewTestApp(t, state, false)
			raw := a.request(t, http.MethodGet, "/.well-known/mnema", nil, "", nil, http.StatusOK)
			var d nativeDiscoveryDocument
			if json.Unmarshal(raw, &d) != nil || len(raw) > 16<<10 || d.Protocol != "mnema-desktop-discovery-v1" || !d.E2EERequired || d.NativeAPI.Compatibility != state || d.NativeAPI.ContentAuthorization != "unavailable" || d.NativeAPI.Prefix != NativePreviewPrefix {
				t.Fatal("native discovery contract invalid")
			}
			if state == NativeUnsupported {
				a.request(t, http.MethodPost, NativePreviewPrefix+"/auth/login", map[string]string{"username": a.user.Username, "password": nativePreviewFixturePassword, "client_instance_id": uuid.NewString()}, "", nil, http.StatusServiceUnavailable)
				return
			}
			grant := a.login(t, a.user)
			for _, path := range []string{"/channels/" + uuid.NewString() + "/messages", "/media/" + uuid.NewString(), "/search", "/webrtc/config", "/auth/password", "/auth/logout-all"} {
				a.request(t, http.MethodGet, NativePreviewPrefix+path, nil, grant.Access, nil, http.StatusServiceUnavailable)
			}
			a.request(t, http.MethodPost, NativePreviewPrefix+"/channels/"+uuid.NewString()+"/upload", map[string]string{"content": "must stay closed"}, grant.Access, nil, http.StatusServiceUnavailable)
		})
	}
}

func TestNativePreviewStrictNativeAndPreservedBrowserBoundaries(t *testing.T) {
	a := newPreviewTestApp(t, NativeSupported, false)
	grant := a.login(t, a.user)
	for _, headers := range []http.Header{{"Origin": []string{a.server.URL}}, {"Origin": []string{""}}, {"Cookie": []string{""}}, {"Authorization": []string{"Bearer " + grant.Access, "Bearer " + grant.Access}}} {
		want := http.StatusForbidden
		if len(headers["Authorization"]) > 0 {
			want = http.StatusUnauthorized
		}
		a.request(t, http.MethodGet, NativePreviewPrefix+"/channels", nil, grant.Access, headers, want)
	}
	for _, path := range []string{"/channels?", "/channels?access_token=invalid", "/channels?limit=1"} {
		a.request(t, http.MethodGet, NativePreviewPrefix+path, nil, grant.Access, nil, http.StatusForbidden)
	}
	a.request(t, http.MethodGet, "/api/channels", nil, grant.Access, nil, http.StatusUnauthorized)
	a.request(t, http.MethodPut, "/api/users/me/status", map[string]string{"status_text": "blocked"}, "", http.Header{"Origin": []string{"https://foreign.example.invalid"}}, http.StatusForbidden)
	// Native endpoints reuse the exact browser login failure state.
	for i := 0; i < 10; i++ {
		a.request(t, http.MethodPost, NativePreviewPrefix+"/auth/login", map[string]string{"username": a.user.Username, "password": "wrong-fixture-password", "client_instance_id": uuid.NewString()}, "", nil, http.StatusUnauthorized)
	}
	a.request(t, http.MethodPost, "/api/auth/login", map[string]string{"username": a.user.Username, "password": nativePreviewFixturePassword}, "", http.Header{"Origin": []string{a.server.URL}}, http.StatusTooManyRequests)
}

func TestNativePreviewWSSSuppressesRealBrowserContentAndDeniesVoice(t *testing.T) {
	a := newPreviewTestApp(t, NativeSupported, true)
	grant := a.login(t, a.user)
	c := a.dial(t, grant)
	previewReadUntil(t, c, "presence_snapshot", nil)
	// Real shared browser handler writes a message; no synthetic policy predicate.
	cookie := httptest.NewRecorder()
	if a.router.preview.accounts.Sessions.Start(cookie, &a.user, 0) != nil {
		t.Fatal("fixture browser session failed")
	}
	channel := uuid.New()
	if _, err := a.pool.Exec(context.Background(), `INSERT INTO channels(id,name,type)VALUES($1,'browser-content-fixture','text')`, channel); err != nil {
		t.Fatal("fixture channel failed")
	}
	a.request(t, http.MethodPost, "/api/channels/"+channel.String()+"/messages", map[string]string{"content": "synthetic browser content must not cross preview"}, "", http.Header{"Origin": []string{a.server.URL}, "Cookie": []string{cookie.Result().Cookies()[0].String()}}, http.StatusCreated)
	rawState := a.request(t, http.MethodGet, NativePreviewPrefix+"/read-state", nil, grant.Access, nil, http.StatusOK)
	var states []map[string]json.RawMessage
	if json.Unmarshal(rawState, &states) != nil || len(states) != 1 || len(states[0]) != 5 {
		t.Fatal("counts-only native read-state schema invalid")
	}
	for _, key := range []string{"channel_id", "last_read_at", "notify_level", "unread_count", "mention_count"} {
		if _, ok := states[0][key]; !ok {
			t.Fatal("unexpected native read-state fields")
		}
	}
	a.request(t, http.MethodPost, NativePreviewPrefix+"/channels/"+channel.String()+"/read", nil, grant.Access, nil, http.StatusServiceUnavailable)
	a.router.Hub.Broadcast("webrtc_offer", map[string]string{"sdp": "synthetic-private-sdp"})
	a.router.Hub.Broadcast("webrtc_media_state", map[string]bool{"screen": true})
	if c.WriteJSON(map[string]any{"type": "ping", "payload": map[string]float64{"t": 123}}) != nil {
		t.Fatal("metadata ping failed")
	}
	previewReadUntil(t, c, "pong", map[string]bool{"message_create": true, "webrtc_offer": true, "webrtc_media_state": true, "voice_snapshot": true, "voice_rooms": true})
	if c.WriteJSON(map[string]any{"type": "voice_join", "payload": map[string]any{"channel_id": channel}}) != nil {
		t.Fatal("forbidden voice request failed")
	}
	previewExpectClosed(t, c)
	if a.router.Hub.SFU.Stats().Peers != 0 {
		t.Fatal("metadata token created an actual SFU peer")
	}
}

func TestNativePreviewRefreshRenewAndLogoutActualSocket(t *testing.T) {
	a := newPreviewTestApp(t, NativeSupported, false)
	original := a.login(t, a.user)
	socket := a.dial(t, original)
	previewReadUntil(t, socket, "presence_snapshot", nil)
	// Mature only the real DB refresh throttle in this isolated fixture; retain
	// genuine token lineage/expiry and run actual HTTP rotation, never sleep.
	if _, err := a.pool.Exec(context.Background(), `UPDATE native_session_families SET refresh_after=now()-interval '1 second' WHERE id=$1`, original.Family); err != nil {
		t.Fatal("fixture refresh budget maturity failed")
	}
	raw := a.request(t, http.MethodPost, NativePreviewPrefix+"/auth/refresh", map[string]string{"refresh_token": original.Refresh}, "", nil, http.StatusOK)
	var next previewTestGrant
	if json.Unmarshal(raw, &next) != nil || next.Family != original.Family || next.Instance != original.Instance || next.Sequence != 1 || next.Access == original.Access {
		t.Fatal("real refresh lineage invalid")
	}
	if socket.WriteJSON(map[string]any{"type": "native_access_renew", "payload": map[string]string{"access_token": next.Access}}) != nil {
		t.Fatal("native renewal send failed")
	}
	previewReadUntil(t, socket, "native_access_renewed", nil)
	a.request(t, http.MethodPost, NativePreviewPrefix+"/auth/logout", nil, next.Access, nil, http.StatusNoContent)
	previewExpectClosed(t, socket)
	a.request(t, http.MethodGet, NativePreviewPrefix+"/members", nil, next.Access, nil, http.StatusUnauthorized)
}

func TestNativePreviewDiscoveryStrictInputsAndNativeInstanceContext(t *testing.T) {
	a := newPreviewTestApp(t, NativeSupported, false)
	for _, headers := range []http.Header{{"Origin": []string{""}}, {"Cookie": []string{""}}, {"Authorization": []string{""}}} {
		a.request(t, http.MethodGet, "/.well-known/mnema", nil, "", headers, http.StatusForbidden)
	}
	a.request(t, http.MethodGet, "/.well-known/mnema?", nil, "", nil, http.StatusForbidden)
	a.request(t, http.MethodPost, "/.well-known/mnema", nil, "", nil, http.StatusMethodNotAllowed)
	grant := a.login(t, a.user)
	// Execute the genuine native DB middleware and context seam, rather than
	// manufacturing a NativePrincipal or a fake eligibility predicate.
	var seen NativePreviewInstance
	called := false
	handler := a.router.Native.RequireUser(a.router.instanceContext(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ok bool
		seen, ok = NativePreviewInstanceFrom(r.Context())
		if !ok {
			t.Error("native authority context absent")
		}
		called = true
		w.WriteHeader(http.StatusNoContent)
	})))
	req := httptest.NewRequest(http.MethodGet, "https://community.example.invalid/probe", nil)
	req.Header.Set("Authorization", "Bearer "+grant.Access)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if !called || response.Code != http.StatusNoContent || seen.CommunityID != "native-preview-fixture" || seen.Origin != a.server.URL || seen.FamilyID != grant.Family || seen.ClientInstanceID != grant.Instance {
		t.Fatal("native instance context did not derive exact server/principal binding")
	}
}

func TestNativePreviewStartupAndAuthorityFailures(t *testing.T) {
	a := newPreviewTestApp(t, NativeSupported, false)
	options := NativePreviewOptions{CommunityID: "native-preview-fixture", InstanceOrigin: a.server.URL, Compatibility: NativeSupported}
	configFor := func() *config.Config {
		cfg, err := config.FromEnv(func(key string) (string, bool) {
			v, ok := map[string]string{"DATABASE_URL": "unused", "PUBLIC_URL": a.server.URL, "JWT_SECRET": "native-preview-fixture-secret-native-preview", "LINK_PREVIEWS_ENABLED": "false"}[key]
			return v, ok
		})
		if err != nil {
			t.Fatal("startup fixture config invalid")
		}
		return cfg
	}
	cfg := configFor()
	cfg.TrustedProxies = []string{"invalid-cidr"}
	if _, err := NewNativePreviewRouter(Deps{Config: cfg, DB: a.pool}, options); err == nil {
		t.Fatal("invalid proxy trust startup admitted")
	}
	cfg = configFor()
	cfg.SessionExpiryHours = 0
	if _, err := NewNativePreviewRouter(Deps{Config: cfg, DB: a.pool}, options); err == nil {
		t.Fatal("invalid native lifetime startup admitted")
	}
	options.CommunityID = ""
	if _, err := NewNativePreviewRouter(Deps{Config: configFor(), DB: a.pool}, options); err == nil {
		t.Fatal("invalid owned options startup admitted")
	}
	req := httptest.NewRequest(http.MethodGet, "https://community.example.invalid", nil)
	res := httptest.NewRecorder()
	a.router.instanceContext(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unauthenticated instance context admitted") })).ServeHTTP(res, req)
	if res.Code != 401 {
		t.Fatal("missing native principal not denied")
	}
	// Registry corruption must fail at assembly, never broaden to all handlers.
	original := nativeMetadataRoutes
	defer func() { nativeMetadataRoutes = original }()
	nativeMetadataRoutes = append(append([]NativeMetadataRoute(nil), original...), NativeMetadataRoute{Method: http.MethodGet, Path: "/not-installed"})
	if _, err := nativeMetadataHandlers(a.router.preview); err == nil {
		t.Fatal("unknown native registry route admitted")
	}
	nativeMetadataRoutes = append(append([]NativeMetadataRoute(nil), original...), original[0])
	if _, err := nativeMetadataHandlers(a.router.preview); err == nil {
		t.Fatal("duplicate native registry route admitted")
	}
}

func TestNativePreviewMetadataEmptyBodiesAreEnforced(t *testing.T) {
	a := newPreviewTestApp(t, NativeSupported, false)
	grant := a.login(t, a.user)
	admin := a.login(t, a.admin)
	for _, path := range []string{"/members", "/channels", "/read-state", "/health", "/legal"} {
		a.request(t, http.MethodGet, NativePreviewPrefix+path, map[string]string{"unexpected": "payload"}, grant.Access, nil, http.StatusBadRequest)
	}
	a.request(t, http.MethodDelete, NativePreviewPrefix+"/admin/categories/"+uuid.NewString(), map[string]string{"unexpected": "payload"}, admin.Access, nil, http.StatusBadRequest)
	a.request(t, http.MethodPost, NativePreviewPrefix+"/admin/users/"+a.user.ID.String()+"/disable", map[string]string{"unexpected": "payload"}, admin.Access, nil, http.StatusBadRequest)
	a.request(t, http.MethodGet, NativePreviewPrefix+"/members", nil, grant.Access, nil, http.StatusOK)
}
