package turnrelay

import (
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAuthorityMintAndCheck(t *testing.T) {
	a, err := newAuthority()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	id := uuid.New()
	user, pass, exp := a.mint(id, now, time.Hour)
	if !exp.Equal(now.Add(time.Hour)) {
		t.Fatalf("expires %v", exp)
	}
	if pass != a.password(user) {
		t.Fatal("password is not derived from the username")
	}
	got, e, err := a.check(user, now.Add(59*time.Minute))
	if err != nil || got != id || !e.Equal(exp) {
		t.Fatalf("check = %v %v %v", got, e, err)
	}
	if _, _, err := a.check(user, now.Add(time.Hour)); err != errExpired {
		t.Fatalf("at the expiry: %v, want expired", err)
	}
	for _, bad := range []string{"", "x", "1:2", "abc:" + id.String(), "0:" + id.String()} {
		if _, _, err := a.check(bad, now); err != errMalformed {
			t.Errorf("check(%q) = %v, want malformed", bad, err)
		}
	}
	other, _ := newAuthority()
	if other.password(user) == pass {
		t.Fatal("two authorities derive the same password")
	}
}

func TestTargetTranslation(t *testing.T) {
	tg := Target{
		LocalIPs:  []net.IP{net.IPv4(10, 0, 0, 5), net.IPv4(127, 0, 0, 1)},
		Announced: []net.IP{net.IPv4(203, 0, 113, 7)},
		PortMin:   50000, PortMax: 50000,
	}
	bind := net.IPv4(10, 0, 0, 5)
	type c struct {
		ip   string
		port int
		ok   bool
		want string
	}
	for _, tc := range []c{
		{"203.0.113.7", 50000, true, "10.0.0.5:50000"},
		{"10.0.0.5", 50000, true, "10.0.0.5:50000"},
		{"127.0.0.1", 50000, true, "127.0.0.1:50000"},
		{"203.0.113.7", 50001, false, ""},
		{"203.0.113.7", 0, false, ""},
		{"8.8.8.8", 50000, false, ""},
		{"10.0.0.6", 50000, false, ""},
	} {
		got, ok := tg.concrete(&net.UDPAddr{IP: net.ParseIP(tc.ip).To4(), Port: tc.port}, bind)
		if ok != tc.ok || (ok && got.String() != tc.want) {
			t.Errorf("concrete(%s:%d) = %v %v, want %v %v", tc.ip, tc.port, got, ok, tc.want, tc.ok)
		}
	}
	if (Target{LocalIPs: tg.LocalIPs}).valid() || (Target{PortMin: 1, PortMax: 1}).valid() || !tg.valid() {
		t.Error("valid() wrong")
	}
	if got := tg.relayIP(); !got.Equal(bind) {
		t.Errorf("relayIP = %v, want the non-loopback address", got)
	}
}

func TestRateLimiter(t *testing.T) {
	l := newRateLimiter(60, 2, 2)
	now := time.Unix(0, 0)
	if !l.allow("a", now) || !l.allow("a", now) || l.allow("a", now) {
		t.Fatal("burst of 2 not enforced")
	}
	if !l.allow("a", now.Add(time.Second)) {
		t.Fatal("no refill after a second")
	}
	if !l.allow("b", now) || l.allow("c", now) {
		t.Fatal("key table is not bounded")
	}
	if !l.allow("c", now.Add(time.Hour)) {
		t.Fatal("idle keys are not forgotten")
	}
}
