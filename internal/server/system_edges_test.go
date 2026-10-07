package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/sfu"
	"github.com/msch128/mnema-talk/internal/update"
)

func TestPublicConfigurationEmptyListsAndSTUN(t *testing.T) {
	rec := httptest.NewRecorder()
	legal(&config.Config{SessionExpiryHours: 25}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/legal", nil))
	if !strings.Contains(rec.Body.String(), `"stun_servers":[]`) || !strings.Contains(rec.Body.String(), `"session_expiry_days":2`) {
		t.Fatal(rec.Body.String())
	}
	rec = httptest.NewRecorder()
	webrtcConfig(&config.Config{WebRTCSTUNURLs: []string{"stun:stun.example.test"}}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/webrtc/config", nil))
	if !strings.Contains(rec.Body.String(), "stun:stun.example.test") || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(rec.Body.String())
	}
}

func TestSFUMetricsCountsBothVideoSources(t *testing.T) {
	var b strings.Builder
	writeSFUMetrics(&b, map[uuid.UUID]map[uuid.UUID]sfu.MediaState{
		uuid.New(): {uuid.New(): {Screen: true, Camera: true}, uuid.New(): {Screen: true}},
		uuid.New(): {uuid.New(): {Camera: true}},
	})
	for _, metric := range []string{"mnema_sfu_video_rooms 2", "mnema_sfu_screen_shares 2", "mnema_sfu_cameras 2"} {
		if !strings.Contains(b.String(), metric) {
			t.Fatal(b.String())
		}
	}
}

func TestUpdateBackoffIsReportedAndEnforced(t *testing.T) {
	var calls atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(http.StatusTooManyRequests) }))
	defer remote.Close()
	checker := update.NewChecker(remote.URL, "0.3.0")
	if err := checker.Check(context.Background()); err == nil {
		t.Fatal("rate limit was accepted")
	}
	h := &systemHandler{updates: checker, version: "0.3.0"}
	status := h.updateStatus()
	if status.RetryAt == nil || status.CheckError == "" {
		t.Fatalf("status=%+v", status)
	}
	rec := httptest.NewRecorder()
	if err := h.checkNow(rec, httptest.NewRequest(http.MethodPost, "/", nil)); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" || calls.Load() != 1 {
		t.Fatalf("response=%d calls=%d", rec.Code, calls.Load())
	}
	// The one-minute manual cooldown wins immediately after an attempt. Once
	// it elapses the longer GitHub backoff must still prevent another request.
	timer := time.NewTimer(update.ManualMinGap + 20*time.Millisecond)
	defer timer.Stop()
	<-timer.C
	rec = httptest.NewRecorder()
	if err := h.checkNow(rec, httptest.NewRequest(http.MethodPost, "/", nil)); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "60" || calls.Load() != 1 {
		t.Fatalf("backoff response=%d retry=%s calls=%d", rec.Code, rec.Header().Get("Retry-After"), calls.Load())
	}
}

func selfUpdateRequest() *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"password":"fixture-password","target_version":"0.4.0"}`))
	return r.WithContext(auth.WithUser(r.Context(), &auth.User{ID: uuid.New(), Username: "fixture-admin", Role: "admin"}))
}

func TestConcurrentSelfUpdatesReserveOneSlot(t *testing.T) {
	releases := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v0.4.0","html_url":"https://github.com/msch128/mnema-talk/releases/tag/v0.4.0"}`))
	}))
	defer releases.Close()
	checker := update.NewChecker(releases.URL, "0.3.0")
	if err := checker.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	sidecarEntered, sidecarRelease := make(chan struct{}), make(chan struct{})
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(sidecarEntered)
		<-sidecarRelease
		w.WriteHeader(http.StatusAccepted)
	}))
	defer sidecar.Close()
	defer func() {
		select {
		case <-sidecarRelease:
		default:
			close(sidecarRelease)
		}
	}()
	updater, err := update.NewUpdater(sidecar.URL, strings.Repeat("fixture-", 5))
	if err != nil {
		t.Fatal(err)
	}
	confirmEntered, confirmRelease := make(chan struct{}, 2), make(chan struct{}, 2)
	h := &systemHandler{cfg: &config.Config{AppImage: "ghcr.io/msch128/mnema-talk:latest"}, version: "0.3.0", updates: checker,
		self: selfUpdate{updater: updater, now: time.Now, confirm: func(context.Context, uuid.UUID, string) (time.Duration, error) {
			confirmEntered <- struct{}{}
			<-confirmRelease
			return 0, nil
		}}}
	results := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		go func() {
			rec := httptest.NewRecorder()
			if err := h.startSelfUpdate(rec, selfUpdateRequest()); err != nil {
				httpx.WriteError(rec, err)
			}
			results <- rec
		}()
	}
	for range 2 {
		select {
		case <-confirmEntered:
		case <-time.After(3 * time.Second):
			t.Fatal("both requests did not reach confirmation")
		}
	}
	confirmRelease <- struct{}{}
	select {
	case <-sidecarEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("updater was not called")
	}
	if retry, blocked := h.selfUpdateBlocked(); !blocked || retry != time.Minute {
		t.Fatalf("in-flight retry=%v blocked=%v", retry, blocked)
	}
	confirmRelease <- struct{}{}
	rejected := <-results
	if rejected.Code != http.StatusTooManyRequests {
		t.Fatalf("concurrent request=%d %s", rejected.Code, rejected.Body.String())
	}
	close(sidecarRelease)
	accepted := <-results
	if accepted.Code != http.StatusAccepted {
		t.Fatalf("first request=%d %s", accepted.Code, accepted.Body.String())
	}
	h.self.lastCall = time.Now().Add(-2 * selfUpdateCooldown)
	if retry, blocked := h.selfUpdateBlocked(); blocked || retry != 0 {
		t.Fatalf("expired retry=%v blocked=%v", retry, blocked)
	}
}

func TestSelfUpdateRejectsMalformedBodyAndSanitizesErrors(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := (&systemHandler{}).startSelfUpdate(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"password":`))); err == nil {
		t.Fatal("malformed JSON accepted")
	}
	if got := updaterMessage(errors.New("private details")); got != "the updater could not start the update; see the updater's log" {
		t.Fatal(got)
	}
	if got := updaterMessage(update.ErrUpdaterUnreachable); got != update.ErrUpdaterUnreachable.Error() {
		t.Fatal(got)
	}
}
