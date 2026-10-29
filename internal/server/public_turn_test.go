package server

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/turnrelay"
	"github.com/pion/turn/v5"
)

func startTestRelay(t *testing.T, mayRelay func(uuid.UUID) bool) *turnrelay.Server {
	t.Helper()
	sfu, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sfu.Close() })
	probe, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.LocalAddr().(*net.UDPAddr).Port
	_ = probe.Close()
	media := uint16(sfu.LocalAddr().(*net.UDPAddr).Port)
	relay, err := turnrelay.Start(turnrelay.Options{
		Port: port, BindAddr: "127.0.0.1", MayRelay: mayRelay,
		Target: func() turnrelay.Target {
			return turnrelay.Target{LocalIPs: []net.IP{net.IPv4(127, 0, 0, 1)}, PortMin: media, PortMax: media}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = relay.Close() })
	return relay
}

// externalPassword is the password the external (coturn) path derives for a
// username from its shared secret.
func externalPassword(secret, username string) string {
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write([]byte(username))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// allocateOn tries a real TURN allocation with the given credentials, which is
// the only way the relay ever checks a password (through message integrity).
func allocateOn(t *testing.T, relay *turnrelay.Server, username, password string) error {
	t.Helper()
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	addr := strings.TrimSuffix(strings.TrimPrefix(relay.URLs("127.0.0.1")[0], "turn:"), "?transport=udp")
	c, err := turn.NewClient(&turn.ClientConfig{TURNServerAddr: addr, Conn: conn, Username: username, Password: password, Realm: turnrelay.Realm})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Listen(); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, err = c.Allocate()
	return err
}

func iceFor(t *testing.T, cfg *config.Config, relay *turnrelay.Server, member uuid.UUID) []IceServer {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/webrtc/config", nil)
	req = req.WithContext(auth.WithUser(req.Context(), &auth.User{ID: member}))
	rec := httptest.NewRecorder()
	webrtcConfig(cfg, relay).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var out ICEConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.ICEServers
}

func TestICEConfigOffersTheBuiltinRelayToMembersWhoMayJoinVoice(t *testing.T) {
	allowed := map[uuid.UUID]bool{}
	relay := startTestRelay(t, func(id uuid.UUID) bool { return allowed[id] })
	cfg := &config.Config{PublicURL: "https://talk.example.test", WebRTCBuiltinTURN: config.BuiltinTURNAuto, WebRTCTURNSecret: "external-secret-0123456789"}

	member, outsider := uuid.New(), uuid.New()
	allowed[member] = true

	got := iceFor(t, cfg, relay, member)
	if len(got) != 1 {
		t.Fatalf("ice servers = %+v, want the built-in relay", got)
	}
	s := got[0]
	if len(s.URLs) != 2 || !strings.HasPrefix(s.URLs[0], "turn:talk.example.test:") ||
		!strings.HasSuffix(s.URLs[0], "?transport=udp") || !strings.HasSuffix(s.URLs[1], "?transport=tcp") {
		t.Fatalf("urls = %v", s.URLs)
	}
	if !strings.HasSuffix(s.Username, ":"+member.String()) || s.Credential == "" {
		t.Fatalf("credential = %q / %q, want one bound to the member", s.Username, s.Credential)
	}
	// Signed by the relay's own secret, never by the external one: the relay
	// accepts it, and what the external secret would sign for the same
	// username is not accepted.
	if err := allocateOn(t, relay, s.Username, s.Credential); err != nil {
		t.Fatalf("the credential is not accepted by the relay's own secret: %v", err)
	}
	if err := allocateOn(t, relay, s.Username, externalPassword(cfg.WebRTCTURNSecret, s.Username)); err == nil {
		t.Fatal("a password derived from the external TURN secret is accepted by the relay")
	}

	if got := iceFor(t, cfg, relay, outsider); len(got) != 0 {
		t.Fatalf("a member who may not join voice was handed %+v", got)
	}
	if got := iceFor(t, cfg, nil, member); len(got) != 0 {
		t.Fatalf("no relay running, yet %+v", got)
	}
}

func TestICEConfigOffersBothWhenForcedOnNextToExternalTURN(t *testing.T) {
	relay := startTestRelay(t, func(uuid.UUID) bool { return true })
	cfg := &config.Config{
		PublicURL:         "https://talk.example.test",
		WebRTCBuiltinTURN: config.BuiltinTURNOn,
		WebRTCTURNURLs:    []string{"turn:turn.example.test:3478?transport=udp"},
		WebRTCTURNSecret:  "external-secret-0123456789",
	}
	got := iceFor(t, cfg, relay, uuid.New())
	if len(got) != 2 {
		t.Fatalf("ice servers = %+v, want external and built-in", got)
	}
	if got[0].URLs[0] != cfg.WebRTCTURNURLs[0] || !strings.HasPrefix(got[1].URLs[0], "turn:talk.example.test:") {
		t.Fatalf("urls = %v / %v", got[0].URLs, got[1].URLs)
	}
	if got[0].Credential == got[1].Credential {
		t.Fatal("both servers share a credential")
	}
}

func TestBuiltinTURNHealth(t *testing.T) {
	h := &systemHandler{cfg: &config.Config{WebRTCBuiltinTURN: config.BuiltinTURNAuto, WebRTCBuiltinTURNPort: 3478, PublicURL: "https://talk.example.test"}}
	st := h.builtinTURNHealth()
	if !st.Enabled || st.Active || st.Mode != "auto" || st.Reachability != "unknown" || st.Host != "talk.example.test" {
		t.Fatalf("not running: %+v", st)
	}
	h.turn = startTestRelay(t, nil)
	st = h.builtinTURNHealth()
	if !st.Active || !st.UDP || !st.TCP {
		t.Fatalf("running: %+v", st)
	}
	h.cfg.WebRTCBuiltinTURN = config.BuiltinTURNOff
	h.turn = nil
	if st := h.builtinTURNHealth(); st.Enabled || st.Active {
		t.Fatalf("off: %+v", st)
	}
	if h.voiceHealth().TURNConfigured {
		t.Fatal("TURN reported configured with the relay off and no external server")
	}
}
