package push

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/httpx"
)

func TestEndpointsOfPushServicesAreAccepted(t *testing.T) {
	for _, raw := range []string{
		"https://fcm.googleapis.com/fcm/send/abc:def",
		"https://updates.push.services.mozilla.com/wpush/v2/gAAAA",
		"https://web.push.apple.com/QOQ=",
		"https://wns2-par02p.notify.windows.com:443/w/?token=abc",
		"  https://push.example.com/x  ",
	} {
		got, err := ValidateEndpoint(raw)
		if err != nil || got != strings.TrimSpace(raw) {
			t.Errorf("%q: %q, %v", raw, got, err)
		}
	}
}

func TestEndpointsThatCouldReachTheServersNetworkAreRefused(t *testing.T) {
	for _, raw := range []string{
		"",
		"http://push.example.com/x",
		"ftp://push.example.com/x",
		"https://push.example.com:8443/x",
		"https://127.0.0.1/x",
		"https://localhost@evil.example/x",
		"https://user:pass@push.example.com/x",
		"https://10.0.0.5/x",
		"https://192.168.1.10/x",
		"https://169.254.169.254/latest/meta-data",
		"https://[::1]/x",
		"https://[fd00::1]/x",
		"https://100.64.0.1/x",
		"https://push.example.com/x#frag",
		"https://push.example.com/a b",
		"push.example.com/x",
		"https://" + strings.Repeat("a", 2100) + ".example.com/x",
	} {
		_, err := ValidateEndpoint(raw)
		ae, ok := httpx.AsAPIError(err)
		if err == nil || !ok || ae.Code != httpx.CodeInvalidInput {
			t.Errorf("%q: %v, want INVALID_INPUT", raw, err)
		}
	}
}

func TestSubscriptionKeysMustBeRealKeys(t *testing.T) {
	b := newBrowser(t)
	secret := make([]byte, 16)
	_, _ = rand.Read(secret)
	padded := base64.URLEncoding.EncodeToString(b.key.PublicKey().Bytes())
	if p, a, err := ValidateKeys(padded, b.secret()+"=="); err != nil || p != b.p256dh() || a != b.secret() {
		t.Fatalf("padded input: %v %q %q", err, p, a)
	}
	notOnCurve := base64.RawURLEncoding.EncodeToString(append([]byte{4}, make([]byte, 64)...))
	for name, c := range map[string][2]string{
		"empty":              {"", ""},
		"not base64":         {"@@@", b.secret()},
		"point not on curve": {notOnCurve, b.secret()},
		"short public key":   {base64.RawURLEncoding.EncodeToString(secret), b.secret()},
		"auth too short":     {b.p256dh(), base64.RawURLEncoding.EncodeToString(secret[:8])},
		"auth too long":      {b.p256dh(), base64.RawURLEncoding.EncodeToString(append(secret, 1))},
		"auth missing":       {b.p256dh(), ""},
	} {
		if _, _, err := ValidateKeys(c[0], c[1]); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestFingerprintIdentifiesAnEndpointWithoutRevealingIt(t *testing.T) {
	a := Fingerprint("https://push.example.com/a")
	if a != Fingerprint("https://push.example.com/a") || a == Fingerprint("https://push.example.com/b") {
		t.Fatal("fingerprint is not a function of the endpoint")
	}
	if len(a) != 32 || strings.Contains(a, "push") {
		t.Fatalf("fingerprint %q", a)
	}
}

func TestVAPIDContactAndKeyShape(t *testing.T) {
	if got := Subject("mailto:ops@example.com", "https://chat.example.com"); got != "mailto:ops@example.com" {
		t.Fatalf("configured contact lost: %q", got)
	}
	if got := Subject("", "https://chat.example.com"); got != "https://chat.example.com" {
		t.Fatalf("https public URL: %q", got)
	}
	if got := Subject("", "http://localhost:8080"); got != fallbackSubject {
		t.Fatalf("plain http falls back: %q", got)
	}
	if subscriber("mailto:ops@example.com") != "ops@example.com" || subscriber("https://x.example") != "https://x.example" {
		t.Fatal("the library adds mailto: itself")
	}
	if !testKeys(t).valid() || (Keys{Public: "abc", Private: "def"}).valid() || (Keys{}).valid() {
		t.Fatal("key shape check")
	}
}
