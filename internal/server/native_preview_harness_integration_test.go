//go:build integration

package server

import (
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// This opt-in loopback fixture is never built into the application binary. It
// exports a public test CA and known dummy credentials, never a private key,
// access token or database address. The normal native transport remains metadata
// only; the separate qualified SFU fixture is not promoted to a content grant.
func TestNativePreviewOperationalHarness(t *testing.T) {
	path := os.Getenv("MNEMA_NATIVE_PREVIEW_BOOTSTRAP")
	if path == "" {
		t.Skip("opt-in retained loopback transport harness")
	}
	if !filepath.IsAbs(path) {
		t.Fatal("fixture bootstrap requires absolute path")
	}
	a := newPreviewTestApp(t, NativeSupported, true)
	bootstrap := struct {
		Protocol             string `json:"protocol"`
		Origin               string `json:"origin"`
		CommunityID          string `json:"community_id"`
		PublicCA             string `json:"public_ca_pem"`
		Username             string `json:"username"`
		Password             string `json:"password"`
		UserID               string `json:"user_id"`
		ContentAuthorization string `json:"content_authorization"`
	}{"mnema-native-loopback-fixture-v1", a.server.URL, "native-preview-fixture", string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: a.server.Certificate().Raw})), a.user.Username, nativePreviewFixturePassword, a.user.ID.String(), "unavailable"}
	raw, err := json.Marshal(bootstrap)
	if err != nil {
		t.Fatal("fixture bootstrap encoding failed")
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal("fixture directory failed")
	}
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal("fixture bootstrap write failed")
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	t.Log("retained verified-TLS native metadata fixture ready")
	// Stop-file ownership is confined to the same ignored bootstrap directory.
	stop := path + ".stop"
	if _, err = os.Stat(stop); err == nil {
		t.Fatal("fixture stop marker already present")
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(4 * time.Hour)
	defer timeout.Stop()
	for {
		select {
		case <-ticker.C:
			if _, err = os.Stat(stop); err == nil {
				_ = os.Remove(stop)
				return
			}
		case <-timeout.C:
			return
		}
	}
}
