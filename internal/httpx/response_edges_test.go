package httpx

import (
	"net/http"
	"net/http/httptest"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/internal/metrics"
)

func TestMetricsCountsRealWebSocketUpgradeAsInformational(t *testing.T) {
	r := chi.NewRouter()
	r.Use(Metrics)
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	r.Get("/ws-metrics-test", func(w http.ResponseWriter, req *http.Request) {
		c, err := up.Upgrade(w, req, nil)
		if err != nil {
			return
		}
		_ = c.Close()
	})
	srv := httptest.NewServer(r)
	defer srv.Close()

	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/ws-metrics-test", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()

	deadline := time.Now().Add(2 * time.Second)
	for metrics.HTTPRequests.With("/ws-metrics-test", "1xx").Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if metrics.HTTPRequests.With("/ws-metrics-test", "1xx").Load() != 1 {
		t.Fatal("hijacked upgrade not counted as 1xx")
	}
	if metrics.HTTPRequests.With("/ws-metrics-test", "2xx").Load() != 0 {
		t.Fatal("hijacked upgrade counted as 2xx")
	}
}

func TestDecodeJSONRejectsStrayClosingBracket(t *testing.T) {
	for _, body := range []string{`{"a":1}}`, `{"a":1}]`, `{"a":1} {"a":2}`, `{"a":1} x`} {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		var dst struct {
			A int `json:"a"`
		}
		if err := DecodeJSON(r, &dst); err == nil {
			t.Fatalf("%q accepted", body)
		}
	}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{\"a\":1}\n "))
	var dst struct {
		A int `json:"a"`
	}
	if err := DecodeJSON(r, &dst); err != nil {
		t.Fatalf("trailing whitespace refused: %v", err)
	}
}

func TestPanicStackKeepsThePanickingFrame(t *testing.T) {
	var stack string
	func() {
		defer func() {
			recover()
			stack = panicStack(debugStackForTest(), 300)
		}()
		var m map[string]int
		m["x"] = 1
	}()
	if !strings.Contains(stack, "TestPanicStackKeepsThePanickingFrame") {
		t.Fatalf("panicking frame cut off: %q", stack)
	}
	if len(stack) > 300 {
		t.Fatalf("stack not bounded: %d", len(stack))
	}
}

func TestCleanNameStripsBlankGlyphsAndRejectsControls(t *testing.T) {
	if _, err := CleanName("name", "ㅤ⠀ᅟ", 32, true); err == nil {
		t.Fatal("blank-glyph-only name accepted")
	}
	if got, err := CleanName("name", "Adㅤmin", 32, true); err != nil || got != "Admin" {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := CleanName("name", "Admin\nSystem notice", 32, true); err == nil {
		t.Fatal("line break accepted")
	}
}

func TestNameSkeletonFoldsHomoglyphs(t *testing.T) {
	for _, in := range []string{"еveryone", "everyone", "EVERYONE", "evеryоне"[:0] + "εveryοne"} {
		if NameSkeleton(in) != "everyone" {
			t.Fatalf("%q -> %q", in, NameSkeleton(in))
		}
	}
	if NameSkeleton("Straße") != "strasse" {
		t.Fatal("sharp s not folded")
	}
}

func TestDiscordCodeExplicitWinsOverMessageWording(t *testing.T) {
	if got := discordCode(ErrNotFound("message thread not found")); got != DiscordCodeUnknownMessage {
		t.Fatalf("fallback = %d", got)
	}
	if got := discordCode(ErrNotFound("message thread not found").WithDiscordCode(DiscordCodeUnknownChannel)); got != DiscordCodeUnknownChannel {
		t.Fatalf("explicit code ignored: %d", got)
	}
	if got := discordCode(ErrInvalidInput("request body is empty").WithDiscordCode(50035)); got != 50035 {
		t.Fatalf("explicit code ignored: %d", got)
	}
}

func TestRetryAfterRoundsUpOnce(t *testing.T) {
	for d, want := range map[time.Duration]string{2 * time.Second: "2", 2100 * time.Millisecond: "3", time.Millisecond: "1"} {
		if got := retryAfterSeconds(d); got != want {
			t.Fatalf("%v -> %s, want %s", d, got, want)
		}
	}
}

func TestIPPrefixKey(t *testing.T) {
	if got := ipPrefixKey("2001:db8:1:2:3:4:5:6", 48); got != "2001:db8:1::/48" {
		t.Fatal(got)
	}
	if got := ipPrefixKey("[::ffff:1.2.3.4]", 64); got != "1.2.3.4" {
		t.Fatal(got)
	}
	if got := ipPrefixKey("nope", 64); got != "nope" {
		t.Fatal(got)
	}
}

func debugStackForTest() []byte { return debug.Stack() }
