package webbundle

import (
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The scheduled workflow runs `trust verify -min-valid-days 60`: a statement
// that is still valid but close to its end must fail the check.
func TestCLITrustVerifyMinValidDays(t *testing.T) {
	dir := t.TempDir()
	if code, _, e := run([]string{"trust", "keygen", "-out", dir}, nil); code != 0 {
		t.Fatal(e)
	}
	bundlePub, _ := testKey(t)
	short := filepath.Join(dir, "short")
	long := filepath.Join(dir, "long")
	for out, validFor := range map[string]string{short: "720h", long: "4320h"} {
		code, _, e := run([]string{"trust", "sign", "-yes", "-key-file", filepath.Join(dir, "offline-root.key"), "-out", out,
			"-bundle-key", hex.EncodeToString(bundlePub), "-valid-for", validFor}, nil)
		if code != 0 {
			t.Fatal(e)
		}
	}
	verify := func(out string, extra ...string) (int, string) {
		args := append([]string{"trust", "verify", "-trust", filepath.Join(out, "trust.json"),
			"-sig", filepath.Join(out, "trust.json.sig"), "-pub", filepath.Join(dir, "offline-root.pub")}, extra...)
		code, stdout, stderr := run(args, nil)
		return code, stdout + stderr
	}
	if code, o := verify(short); code != 0 {
		t.Fatalf("plain verify of a valid statement failed: %s", o)
	}
	if code, o := verify(short, "-min-valid-days", "60"); code == 0 || !strings.Contains(o, "renew") {
		t.Fatalf("30 days left passed the 60 day check: %d %s", code, o)
	}
	if code, o := verify(short, "-min-valid-days", "10"); code != 0 {
		t.Fatalf("30 days left failed the 10 day check: %s", o)
	}
	if code, o := verify(long, "-min-valid-days", "60"); code != 0 {
		t.Fatalf("180 days left failed the 60 day check: %s", o)
	}
}

func TestCheckRemainingRejectsBadExpiry(t *testing.T) {
	if err := checkRemaining(&Trust{ExpiresAt: "tomorrow"}, time.Now(), 60); err == nil {
		t.Fatal("unparsable expiry accepted")
	}
	if err := checkRemaining(&Trust{ExpiresAt: "tomorrow"}, time.Now(), 0); err != nil {
		t.Fatal("disabled check must not look at the date")
	}
}
