//go:build integration

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/testutil"
)

func newPublicPreviewTestApp(t *testing.T, status NativeCompatibility) *previewTestApp {
	t.Helper()
	pool := testutil.DB(t)
	testutil.Reset(t, pool)
	server := httptest.NewUnstartedServer(nil)
	origin := "https://" + server.Listener.Addr().String()
	cfg, err := config.FromEnv(func(key string) (string, bool) {
		v, ok := map[string]string{"DATABASE_URL": "unused", "PUBLIC_URL": origin, "JWT_SECRET": "native-public-fixture-secret-native-public", "LEGAL_OPERATOR_NAME": "Synthetic Community Operator", "LEGAL_OPERATOR_EMAIL": "operator@example.invalid", "LEGAL_PROJECT_NOTICE": "Public synthetic fixture terms", "MEDIA_RETENTION_DAYS": "0"}[key]
		return v, ok
	})
	if err != nil {
		t.Fatal("public fixture config failed")
	}
	router, err := NewNativePublicPreviewRouter(Deps{Config: cfg, DB: pool, Version: "public-preview-fixture"}, NativePreviewOptions{CommunityID: "native-public-preview-fixture", InstanceOrigin: origin, Compatibility: status})
	if err != nil {
		t.Fatal("actual public router construction failed")
	}
	server.Config.Handler = router
	server.Config.ReadHeaderTimeout = 5 * time.Second
	server.Config.ReadTimeout = 5 * time.Second
	server.StartTLS()
	t.Cleanup(server.Close)
	t.Cleanup(router.Close)
	if server.Client().Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify {
		t.Fatal("public fixture TLS disabled")
	}
	return &previewTestApp{server: server, router: router, pool: pool}
}
func TestNativePublicPreviewActualHTTPSBeforeLogin(t *testing.T) {
	a := newPublicPreviewTestApp(t, NativeSupported)
	var users, sessions int
	if a.pool.QueryRow(context.Background(), `SELECT count(*) FROM users`).Scan(&users) != nil || a.pool.QueryRow(context.Background(), `SELECT count(*) FROM native_session_families`).Scan(&sessions) != nil || users != 0 || sessions != 0 {
		t.Fatal("prelogin fixture unexpectedly authenticated")
	}
	raw := a.request(t, http.MethodGet, NativePublicLegalPath, nil, "", nil, http.StatusOK)
	var legal Legal
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &legal) != nil || json.Unmarshal(raw, &fields) != nil || len(fields) != 10 || legal.OperatorName != "Synthetic Community Operator" || legal.OperatorEmail != "operator@example.invalid" || legal.MediaRetentionDays != 0 || legal.LegalVersion != LegalVersion {
		t.Fatal("real public legal DTO mismatch")
	}
	raw = a.request(t, http.MethodGet, NativePublicHealthPath, nil, "", nil, http.StatusOK)
	var health Health
	if json.Unmarshal(raw, &health) != nil || health.Status != "ok" || health.Version != "public-preview-fixture" {
		t.Fatal("real public health DTO mismatch")
	}
	a.request(t, http.MethodGet, NativePreviewPrefix+"/legal", nil, "", nil, http.StatusUnauthorized)
	a.request(t, http.MethodGet, NativePreviewPrefix+"/health", nil, "", nil, http.StatusUnauthorized)
	a.request(t, http.MethodGet, NativePreviewPrefix+"/members", nil, "", nil, http.StatusUnauthorized)
	a.request(t, http.MethodGet, "/api/legal", nil, "", nil, http.StatusOK)
	a.request(t, http.MethodGet, "/api/health", nil, "", nil, http.StatusOK)
	if a.pool.QueryRow(context.Background(), `SELECT count(*) FROM native_session_families`).Scan(&sessions) != nil || sessions != 0 {
		t.Fatal("public metadata created a native session")
	}
}
func TestNativePublicPreviewActualHTTPSStrictBoundary(t *testing.T) {
	a := newPublicPreviewTestApp(t, NativeSupported)
	for _, path := range []string{NativePublicLegalPath, NativePublicHealthPath} {
		for _, headers := range []http.Header{{"Origin": []string{""}}, {"Cookie": []string{""}}, {"Authorization": []string{""}}, {"Authorization": []string{"Bearer " + strings.Repeat("A", 43)}}} {
			a.request(t, http.MethodGet, path, nil, "", headers, http.StatusForbidden)
		}
		a.request(t, http.MethodGet, path+"?", nil, "", nil, http.StatusForbidden)
		a.request(t, http.MethodGet, path+"?profile="+uuid.NewString(), nil, "", nil, http.StatusForbidden)
		a.request(t, http.MethodGet, path, map[string]string{"unexpected": "payload"}, "", nil, http.StatusBadRequest)
		a.request(t, http.MethodPost, path, nil, "", nil, http.StatusMethodNotAllowed)
		// Real chunked GET: denied from framing before waiting/consuming its payload.
		req, err := http.NewRequest(http.MethodGet, a.server.URL+path, io.NopCloser(bytes.NewReader([]byte("x"))))
		if err != nil {
			t.Fatal("chunked fixture request failed")
		}
		req.ContentLength = -1
		req.TransferEncoding = []string{"chunked"}
		res, err := a.server.Client().Do(req)
		if err != nil {
			t.Fatal("real chunked HTTPS failed")
		}
		_ = res.Body.Close()
		if res.StatusCode != http.StatusBadRequest || res.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("chunked public body not rejected")
		}
	}
}
func TestNativePublicPreviewCompatibility(t *testing.T) {
	for _, status := range []NativeCompatibility{NativeSupported, NativeDeprecated, NativeUnsupported} {
		t.Run(string(status), func(t *testing.T) {
			a := newPublicPreviewTestApp(t, status)
			want := http.StatusOK
			if status == NativeUnsupported {
				want = http.StatusServiceUnavailable
			}
			a.request(t, http.MethodGet, NativePublicLegalPath, nil, "", nil, want)
			a.request(t, http.MethodGet, NativePublicHealthPath, nil, "", nil, want)
		})
	}
}
