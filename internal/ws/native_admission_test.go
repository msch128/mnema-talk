package ws

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/httpx"
)

func TestNativeSocketUnavailableAndUnexpectedAuthErrorRedacted(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	defer h.Close()
	response := httptest.NewRecorder()
	h.NativeWebSocketHandler(nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "https://example.invalid/api/native/v1/ws", nil))
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("unavailable native socket/cache boundary failed")
	}
	response = httptest.NewRecorder()
	const marker = "internal-cause-must-never-be-a-wire-error"
	writeNativeAuthError(response, errors.New(marker))
	if response.Code != http.StatusUnauthorized || strings.Contains(response.Body.String(), marker) {
		t.Fatal("unclassified native cause reached wire response")
	}
	response = httptest.NewRecorder()
	writeNativeAuthError(response, httpx.ErrForbidden("native boundary"))
	if response.Code != http.StatusForbidden {
		t.Fatal("explicit API error lost its sanitized status")
	}
}

type nativeBrokenBody struct{}

func (nativeBrokenBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (nativeBrokenBody) Close() error             { return nil }

func TestNativeHeaderPresenceRejectsNoncanonicalAndEmptyKeys(t *testing.T) {
	for _, key := range []string{"origin", "ORIGIN", "cookie", "COOKIE", "sec-websocket-protocol"} {
		r := httptest.NewRequest(http.MethodGet, "https://example.invalid/api/native/v1/ws", nil)
		r.Header[key] = nil
		if nativeHandshakeAllowed(r) {
			t.Fatal("present empty noncanonical native header accepted")
		}
	}
}
