package ssrf

import (
	"net/netip"
	"testing"
)

// FuzzValidateURL checks the URL guard never panics and, with the default
// policy, never accepts a URL whose literal host is a refused address.
func FuzzValidateURL(f *testing.F) {
	for _, s := range []string{
		"https://example.com/", "http://127.0.0.1/", "http://[::1]/", "http://[::ffff:127.0.0.1]/", "http://169.254.169.254/latest",
		"http://0x7f.1/", "http://2130706433/", "http://localhost./", "http://user:pw@example.com/", "ftp://example.com/",
		"http://[fe80::1%25eth0]/", "http://10.0.0.1:80/", "//example.com", "http:example.com", "https://exa mple.com", "",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		p := &Policy{}
		u, err := p.ValidateURL(raw)
		if err != nil {
			return
		}
		if (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Hostname() == "" {
			t.Fatalf("accepted %q as %v", raw, u)
		}
		if a, perr := netip.ParseAddr(u.Hostname()); perr == nil && Refused(a) {
			t.Fatalf("accepted refused literal %q", raw)
		}
	})
}

// FuzzParseAllow checks the allowlist parser never panics and its prefixes are valid.
func FuzzParseAllow(f *testing.F) {
	for _, s := range []string{"", "10.0.0.0/8", "10.0.0.1, 192.168.0.0/16", "::1", "fe80::/10,,", "1.2.3.4/33", "garbage", "::ffff:10.0.0.1"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, list string) {
		prefixes, err := ParseAllow(list)
		if err != nil {
			return
		}
		for _, p := range prefixes {
			if !p.IsValid() {
				t.Fatalf("invalid prefix from %q", list)
			}
		}
	})
}
