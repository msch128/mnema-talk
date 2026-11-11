package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientAggregateKey(t *testing.T) {
	for _, tc := range []struct{ remote, want string }{
		{"203.0.113.9:1", "203.0.113.9"},
		{"[2001:db8:1:5::7]:1", "2001:db8:1::/48"},
		{"[2001:db8:1:ffff::1]:1", "2001:db8:1::/48"},
		{"[2001:db8:2::1]:1", "2001:db8:2::/48"},
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = tc.remote
		if got := ClientAggregateKey(r); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.remote, got, tc.want)
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
