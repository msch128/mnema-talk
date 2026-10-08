package server

import (
	"github.com/msch128/mnema-talk/internal/config"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNativePublicPreviewHeaderAndBodyBoundary(t *testing.T) {
	cases := []struct {
		auth    bool
		body    string
		length  int64
		chunked bool
		want    int
	}{{false, "", 0, false, 204}, {true, "", 0, false, 403}, {false, "x", 1, false, 400}, {false, "", -1, true, 400}}
	for i, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "https://community.example.invalid/api/native/v1/public/legal", nil)
		if c.auth {
			r.Header["authorization"] = []string{""}
		}
		if c.body != "" {
			r.Body = http.NoBody
			r.ContentLength = c.length
		}
		if c.chunked {
			r.ContentLength = c.length
			r.TransferEncoding = []string{"chunked"}
		}
		response := httptest.NewRecorder()
		nativePublicPreviewBoundary(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })).ServeHTTP(response, r)
		if response.Code != c.want {
			t.Fatalf("public boundarycase%d status%d expected%d", i, response.Code, c.want)
		}
	}
	// A reader which claims zero content length is still not a known-empty body.
	r := httptest.NewRequest(http.MethodGet, "https://community.example.invalid", strings.NewReader("x"))
	r.ContentLength = 0
	response := httptest.NewRecorder()
	nativePublicPreviewBoundary(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unknown body admitted") })).ServeHTTP(response, r)
	if response.Code != 400 {
		t.Fatal("unknown body not denied")
	}
	if _, err := NewNativePublicPreviewRouter(Deps{}, NativePreviewOptions{}); err == nil {
		t.Fatal("invalid public preview constructor admitted")
	}
}

func TestNativePublicPreviewInvalidStartupTrust(t *testing.T) {
	if _, err := NewNativePublicPreviewRouter(Deps{Config: &config.Config{TrustedProxies: []string{"invalid-cidr"}}}, NativePreviewOptions{}); err == nil {
		t.Fatal("invalid public proxy trust accepted")
	}
	if _, err := NewNativePublicPreviewRouter(Deps{Config: &config.Config{}}, NativePreviewOptions{}); err == nil {
		t.Fatal("invalid basepreview constructor accepted")
	}
}
