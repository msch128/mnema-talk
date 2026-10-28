package ssrf

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func addr(s string) netip.Addr { return netip.MustParseAddr(s) }

func TestCheckRefusesEverythingButPublicUnicast(t *testing.T) {
	var p *Policy
	blocked := []string{
		// loopback, unspecified
		"127.0.0.1", "127.255.255.254", "::1", "0.0.0.0", "::", "0.1.2.3",
		// RFC 1918
		"10.0.0.1", "10.255.255.255", "172.16.0.1", "172.31.255.255", "192.168.0.1", "192.168.255.255",
		// link-local, metadata
		"169.254.0.1", "169.254.169.254", "fe80::1", "fd00:ec2::254", "100.100.100.200", "192.0.0.192",
		// CGNAT, benchmarking, documentation, reserved
		"100.64.0.1", "100.127.255.255", "198.18.0.1", "198.19.255.255", "192.0.2.1", "198.51.100.1", "203.0.113.1", "192.0.0.1", "192.88.99.1", "240.0.0.1", "255.255.255.255",
		// multicast
		"224.0.0.1", "239.255.255.255", "ff02::1", "ff0e::1",
		// IPv6 unique local, site-local, discard, documentation, tunnels
		"fc00::1", "fdff::1", "fec0::1", "100::1", "2001:db8::1", "2001::1", "2002:c000:204::1", "64:ff9b::808:808", "64:ff9b:1::1",
		// IPv4-mapped and -compatible forms of the above
		"::ffff:127.0.0.1", "::ffff:10.0.0.1", "::ffff:169.254.169.254", "::ffff:192.168.1.1", "::ffff:0.0.0.0", "::10.0.0.1", "::127.0.0.1",
		// with a zone
		"fe80::1%eth0",
	}
	for _, s := range blocked {
		a, err := netip.ParseAddr(s)
		if err != nil {
			t.Fatalf("bad test address %q: %v", s, err)
		}
		if err := p.Check(a); !errors.Is(err, ErrBlocked) {
			t.Errorf("%s was not blocked", s)
		}
	}
	for _, s := range []string{"93.184.216.34", "8.8.8.8", "1.1.1.1", "172.15.255.255", "172.32.0.1", "100.63.255.255", "100.128.0.1", "2606:4700:4700::1111", "2a00:1450:4001::200e", "::ffff:8.8.8.8"} {
		if err := p.Check(addr(s)); err != nil {
			t.Errorf("%s was blocked: %v", s, err)
		}
	}
	if err := p.Check(netip.Addr{}); !errors.Is(err, ErrBlocked) {
		t.Error("the zero address was not blocked")
	}
}

func TestRefusedSharedNonPublicAddresses(t *testing.T) {
	cases := []struct {
		name string
		addr string
		want bool
	}{
		{name: "loopback", addr: "127.0.0.1", want: true},
		{name: "loopback v6", addr: "::1", want: true},
		{name: "link-local", addr: "169.254.1.1", want: true},
		{name: "link-local v6", addr: "fe80::1", want: true},
		{name: "multicast", addr: "224.0.0.1", want: true},
		{name: "multicast v6", addr: "ff02::1", want: true},
		{name: "unspecified", addr: "0.0.0.0", want: true},
		{name: "unspecified v6", addr: "::", want: true},
		{name: "cloud metadata", addr: "169.254.169.254", want: true},
		{name: "cloud metadata v6", addr: "fd00:ec2::254", want: true},
		{name: "cloud metadata alibaba", addr: "100.100.100.200", want: true},
		{name: "cloud metadata oracle", addr: "192.0.0.192", want: true},
		{name: "cloud metadata mapped", addr: "::ffff:169.254.169.254", want: true},
		{name: "public", addr: "93.184.216.34", want: false},
		{name: "public v6", addr: "2606:4700::1111", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Refused(netip.MustParseAddr(tc.addr))
			if got != tc.want {
				t.Errorf("Refused(%s) = %v, want %v", tc.addr, got, tc.want)
			}
		})
	}
	if !Refused(netip.Addr{}) {
		t.Error("the zero address was not refused")
	}
}

func TestAllowOpensOnlyLocalRanges(t *testing.T) {
	allow, err := ParseAllow(" 192.168.1.0/24, 10.1.2.3 ,fd12::/16, ::ffff:172.16.0.5,, ")
	if err != nil {
		t.Fatal(err)
	}
	p := &Policy{Allow: allow}
	for _, s := range []string{"192.168.1.77", "10.1.2.3", "fd12::5", "172.16.0.5", "::ffff:192.168.1.1"} {
		if err := p.Check(addr(s)); err != nil {
			t.Errorf("%s should be allowed: %v", s, err)
		}
	}
	for _, s := range []string{"192.168.2.1", "10.1.2.4", "127.0.0.1", "fd13::1"} {
		if err := p.Check(addr(s)); err == nil {
			t.Errorf("%s should still be blocked", s)
		}
	}
	// Nothing can open the metadata services or the reserved ranges.
	all, err := ParseAllow("0.0.0.0/0, ::/0")
	if err != nil {
		t.Fatal(err)
	}
	open := &Policy{Allow: all}
	for _, s := range []string{"169.254.169.254", "fd00:ec2::254", "100.100.100.200", "192.0.0.192", "0.0.0.0", "224.0.0.1", "::", "ff02::1", "192.0.2.1", "64:ff9b::808:808", "::ffff:169.254.169.254"} {
		if err := open.Check(addr(s)); err == nil {
			t.Errorf("%s was opened by an allowlist", s)
		}
	}
	if err := open.Check(addr("127.0.0.1")); err != nil {
		t.Errorf("loopback with 0.0.0.0/0 allowed: %v", err)
	}
	if _, err := ParseAllow("10.0.0.0/8, nonsense"); err == nil {
		t.Error("garbage accepted")
	}
	if got, err := ParseAllow(""); err != nil || got != nil {
		t.Errorf("empty list: %v %v", got, err)
	}
}

func TestValidateURL(t *testing.T) {
	allow, _ := ParseAllow("192.168.1.0/24")
	p := &Policy{Allow: allow}
	good := []string{
		"https://example.com/hook", "http://example.com:8080/a?b=c", "https://hooks.example.org", "http://192.168.1.5/x", "https://[2606:4700:4700::1111]/x",
		"http://localhost/x", // a name: resolved and refused when it is dialled
	}
	for _, s := range good {
		if _, err := p.ValidateURL(s); err != nil {
			t.Errorf("%q rejected: %v", s, err)
		}
	}
	bad := map[string]string{
		"empty":        "",
		"spaces":       "   ",
		"ftp":          "ftp://example.com/",
		"file":         "file:///etc/passwd",
		"javascript":   "javascript:alert(1)",
		"gopher":       "gopher://example.com",
		"no host":      "https:///path",
		"relative":     "/hook",
		"schemeless":   "example.com/hook",
		"userinfo":     "https://user:pass@example.com/",
		"opaque":       "https:example.com",
		"inner space":  "https://example.com/a b",
		"control":      "https://example.com/a\x01b",
		"newline":      "https://example.com/\nHost: evil",
		"loopback":     "http://127.0.0.1/",
		"loopback v6":  "http://[::1]/",
		"private":      "http://10.0.0.1/",
		"metadata":     "http://169.254.169.254/latest/meta-data/",
		"metadata v6":  "http://[fd00:ec2::254]/",
		"mapped":       "http://[::ffff:127.0.0.1]/",
		"unspecified":  "http://0.0.0.0/",
		"too long":     "https://example.com/" + strings.Repeat("a", maxURLLen),
		"not allowed":  "http://192.168.2.5/",
		"garbage port": "http://example.com:notaport/",
	}
	for name, s := range bad {
		if _, err := p.ValidateURL(s); err == nil {
			t.Errorf("%s (%q) accepted", name, s)
		}
	}
}

// fakeResolver answers from a script, one answer per lookup.
type fakeResolver struct {
	mu      sync.Mutex
	answers [][]netip.Addr
	err     error
	calls   int
}

func (f *fakeResolver) LookupNetIP(_ context.Context, _, _ string) ([]netip.Addr, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	i := min(f.calls, len(f.answers)-1)
	f.calls++
	return f.answers[i], nil
}

type dialLog struct {
	mu    sync.Mutex
	addrs []string
}

func (l *dialLog) dial(_ context.Context, _, address string) (net.Conn, error) {
	l.mu.Lock()
	l.addrs = append(l.addrs, address)
	l.mu.Unlock()
	return nil, errors.New("refused for the test")
}

func TestDialerResolvesChecksAndPins(t *testing.T) {
	cases := []struct {
		name    string
		answers []netip.Addr
		err     error
		want    error
		dialed  []string
	}{
		{"public", []netip.Addr{addr("93.184.216.34")}, nil, ErrConnect, []string{"93.184.216.34:443"}},
		{"private answer", []netip.Addr{addr("10.0.0.5")}, nil, ErrBlocked, nil},
		{"loopback answer", []netip.Addr{addr("127.0.0.1")}, nil, ErrBlocked, nil},
		{"metadata answer", []netip.Addr{addr("169.254.169.254")}, nil, ErrBlocked, nil},
		{"mapped answer", []netip.Addr{addr("::ffff:10.0.0.5")}, nil, ErrBlocked, nil},
		{"mixed answers are refused as a whole", []netip.Addr{addr("93.184.216.34"), addr("192.168.1.1")}, nil, ErrBlocked, nil},
		{"two public answers are tried in turn", []netip.Addr{addr("93.184.216.34"), addr("2606:4700:4700::1111")}, nil, ErrConnect, []string{"93.184.216.34:443", "[2606:4700:4700::1111]:443"}},
		{"no answer", []netip.Addr{}, nil, ErrResolve, nil},
		{"lookup error", nil, errors.New("nxdomain"), ErrResolve, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := &fakeResolver{answers: [][]netip.Addr{tc.answers}, err: tc.err}
			log := &dialLog{}
			d := &Dialer{Policy: nil, Resolver: res, dial: log.dial}
			_, err := d.DialContext(context.Background(), "tcp", "hook.example:443")
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if strings.Join(log.addrs, ",") != strings.Join(tc.dialed, ",") {
				t.Fatalf("dialed %v, want %v", log.addrs, tc.dialed)
			}
		})
	}
}

// DNS rebinding: a name that is public when checked and private when
// connected to. The dialer resolves once and connects by number, so the
// second answer is never asked for, and a client that looks the name up
// again for every connection gets the check again.
func TestDNSRebindingCannotReachTheSecondAnswer(t *testing.T) {
	res := &fakeResolver{answers: [][]netip.Addr{{addr("93.184.216.34")}, {addr("127.0.0.1")}}}
	log := &dialLog{}
	d := &Dialer{Resolver: res, dial: log.dial}
	_, _ = d.DialContext(context.Background(), "tcp", "rebind.example:80")
	if res.calls != 1 || len(log.addrs) != 1 || log.addrs[0] != "93.184.216.34:80" {
		t.Fatalf("lookups %d, dialed %v", res.calls, log.addrs)
	}
	// The next connection resolves again, gets the private answer and is refused.
	if _, err := d.DialContext(context.Background(), "tcp", "rebind.example:80"); !errors.Is(err, ErrBlocked) {
		t.Fatalf("second connection: %v", err)
	}
	if len(log.addrs) != 1 {
		t.Fatalf("private address was dialled: %v", log.addrs)
	}
}

func TestDialerLiteralsAndBadAddresses(t *testing.T) {
	log := &dialLog{}
	d := &Dialer{Resolver: &fakeResolver{err: errors.New("must not be asked")}, dial: log.dial}
	if _, err := d.DialContext(context.Background(), "tcp", "127.0.0.1:80"); !errors.Is(err, ErrBlocked) {
		t.Fatalf("literal loopback: %v", err)
	}
	if _, err := d.DialContext(context.Background(), "tcp", "[::ffff:10.1.1.1]:80"); !errors.Is(err, ErrBlocked) {
		t.Fatalf("literal mapped: %v", err)
	}
	if _, err := d.DialContext(context.Background(), "tcp", "8.8.8.8:53"); !errors.Is(err, ErrConnect) || log.addrs[0] != "8.8.8.8:53" {
		t.Fatalf("literal public: %v %v", err, log.addrs)
	}
	if _, err := d.DialContext(context.Background(), "tcp", "no-port"); !errors.Is(err, ErrBadURL) {
		t.Fatalf("no port: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	two := &Dialer{Resolver: &fakeResolver{answers: [][]netip.Addr{{addr("8.8.8.8"), addr("8.8.4.4")}}}, dial: log.dial}
	before := len(log.addrs)
	if _, err := two.DialContext(ctx, "tcp", "x.example:1"); !errors.Is(err, ErrConnect) || len(log.addrs) != before+1 {
		t.Fatalf("cancelled context keeps trying: %v %v", err, log.addrs)
	}
}

func TestNewDialerUsesTheSystemResolver(t *testing.T) {
	d := NewDialer(nil)
	if d.Resolver != net.DefaultResolver || d.Timeout <= 0 {
		t.Fatalf("%+v", d)
	}
	// localhost resolves to loopback addresses, which the guard refuses.
	if _, err := d.DialContext(context.Background(), "tcp", "localhost:80"); !errors.Is(err, ErrBlocked) {
		t.Fatalf("localhost: %v", err)
	}
}

// End to end against a real local server: allowed only through the allowlist,
// no redirects, no proxy.
func TestClientEndToEnd(t *testing.T) {
	var hits int
	var mu sync.Mutex
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "http://169.254.169.254/latest", http.StatusFound)
		default:
			_, _ = io.WriteString(w, "ok")
		}
	}))
	defer target.Close()
	u, _ := url.Parse(target.URL)
	port := u.Port()

	local := &fakeResolver{answers: [][]netip.Addr{{addr("127.0.0.1")}}}
	closed := NewClient(&Dialer{Policy: &Policy{}, Resolver: local, Timeout: time.Second}, 2*time.Second)
	if _, err := closed.Get("http://hook.example:" + port + "/"); err == nil || !strings.Contains(err.Error(), ErrBlocked.Error()) {
		t.Fatalf("loopback target reached without allowlist: %v", err)
	}
	if hits != 0 {
		t.Fatalf("%d requests reached the target", hits)
	}

	allow, _ := ParseAllow("127.0.0.0/8")
	open := NewClient(&Dialer{Policy: &Policy{Allow: allow}, Resolver: local, Timeout: time.Second}, 2*time.Second)
	res, err := open.Get("http://hook.example:" + port + "/")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 || hits != 1 {
		t.Fatalf("status %d, hits %d", res.StatusCode, hits)
	}
	// A redirect is returned, not followed.
	res, err = open.Get("http://hook.example:" + port + "/redirect")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusFound || hits != 2 {
		t.Fatalf("redirect: status %d, hits %d", res.StatusCode, hits)
	}
	// Proxy variables are not used.
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("http_proxy", "http://127.0.0.1:1")
	res, err = open.Get("http://hook.example:" + port + "/")
	if err != nil {
		t.Fatalf("proxy environment used: %v", err)
	}
	res.Body.Close()
}

func TestAllowCanOpenIPv6LoopbackButNotIPv4Compatible(t *testing.T) {
	allow, err := ParseAllow("::1")
	if err != nil {
		t.Fatal(err)
	}
	p := &Policy{Allow: allow}
	if err := p.Check(addr("::1")); err != nil {
		t.Errorf("::1 on the allowlist: %v", err)
	}
	if err := (&Policy{}).Check(addr("::1")); err == nil {
		t.Error("::1 allowed without an allowlist")
	}
	all, _ := ParseAllow("::/0")
	for _, s := range []string{"::", "::1.2.3.4", "::808:808"} {
		if err := (&Policy{Allow: all}).Check(addr(s)); err == nil {
			t.Errorf("%s was opened by an allowlist", s)
		}
	}
	if PublicUnicast(addr("::1.2.3.4")) || PublicUnicast(addr("::1")) {
		t.Error("IPv4-compatible or loopback treated as public")
	}
}

func TestParseAllowIPv4MappedPrefixMatchesIPv4(t *testing.T) {
	allow, err := ParseAllow("::ffff:10.0.0.0/104")
	if err != nil {
		t.Fatal(err)
	}
	p := &Policy{Allow: allow}
	if err := p.Check(addr("10.1.2.3")); err != nil {
		t.Errorf("mapped prefix did not match 10.1.2.3: %v", err)
	}
	if err := p.Check(addr("192.168.1.1")); err == nil {
		t.Error("192.168.1.1 allowed by the 10/8 prefix")
	}
	if _, err := ParseAllow("::ffff:0:0/64"); err == nil {
		t.Error("a mapped prefix shorter than /96 was accepted")
	}
}
