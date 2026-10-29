package server

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestOrigin(t *testing.T) {
	plain := httptest.NewRequest(http.MethodGet, "http://chat.example.com/api/admin/status", nil)
	if got := requestOrigin(plain); got != "http://chat.example.com" {
		t.Errorf("plain request = %q", got)
	}
	proxied := httptest.NewRequest(http.MethodGet, "http://chat.example.com/api/admin/status", nil)
	proxied.Header.Set("X-Forwarded-Proto", "HTTPS")
	if got := requestOrigin(proxied); got != "https://chat.example.com" {
		t.Errorf("proxied request = %q", got)
	}
	direct := httptest.NewRequest(http.MethodGet, "https://chat.example.com:8443/", nil)
	direct.TLS = &tls.ConnectionState{}
	if got := requestOrigin(direct); got != "https://chat.example.com:8443" {
		t.Errorf("TLS request = %q", got)
	}
}
