package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var testToken = strings.Repeat("tok-", 8)

func TestUpdaterTriggersTheSidecar(t *testing.T) {
	var gotMethod, gotPath, gotQuery, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotQuery, gotAuth = r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	u, err := NewUpdater(srv.URL+"/", testToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := u.Trigger(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/v1/update" || gotQuery != "async=true" || gotAuth != "Bearer "+testToken {
		t.Fatalf("request: %s %s?%s auth=%q", gotMethod, gotPath, gotQuery, gotAuth)
	}
}

func TestUpdaterErrors(t *testing.T) {
	for status, want := range map[int]error{
		http.StatusUnauthorized:    ErrUpdaterAuth,
		http.StatusTooManyRequests: ErrUpdaterBusy,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte("secret detail " + testToken))
		}))
		u, _ := NewUpdater(srv.URL, testToken)
		if err := u.Trigger(context.Background()); !errors.Is(err, want) {
			t.Errorf("status %d: err = %v, want %v", status, err, want)
		}
		srv.Close()
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(testToken))
	}))
	u, _ := NewUpdater(srv.URL, testToken)
	err := u.Trigger(context.Background())
	if err == nil || strings.Contains(err.Error(), testToken) {
		t.Fatalf("500: err = %v (must not echo the body or token)", err)
	}
	srv.Close()

	// Closed server: unreachable.
	if err := u.Trigger(context.Background()); !errors.Is(err, ErrUpdaterUnreachable) {
		t.Fatalf("closed: err = %v", err)
	}
}

func TestUpdaterNeverFollowsRedirects(t *testing.T) {
	var leaked bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("Authorization") != ""
		w.WriteHeader(http.StatusAccepted)
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/v1/update", http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	u, _ := NewUpdater(srv.URL, testToken)
	if err := u.Trigger(context.Background()); err == nil || leaked {
		t.Fatalf("redirect followed: err=%v leaked=%v", err, leaked)
	}
}

func TestNewUpdaterValidatesURL(t *testing.T) {
	for _, raw := range []string{"", "ftp://x", "http://user:pw@x", "not a url", "http://"} {
		if _, err := NewUpdater(raw, testToken); err == nil {
			t.Errorf("NewUpdater(%q) accepted", raw)
		}
	}
}

func TestImageReach(t *testing.T) {
	for _, tc := range []struct {
		image, latest string
		reach         Reach
		reason        string
	}{
		{"", "0.4.0", ReachUnknown, ReasonUnset},
		{"mnema-talk:local", "0.4.0", ReachNo, ReasonLocal},
		{"ghcr.io/msch128/mnema-talk:latest", "0.4.0", ReachYes, ReasonFollows},
		{"ghcr.io/msch128/mnema-talk", "0.4.0", ReachYes, ReasonFollows},
		{"ghcr.io/msch128/mnema-talk:0.4", "0.4.2", ReachYes, ReasonFollows},
		{"ghcr.io/msch128/mnema-talk:0.3", "0.4.0", ReachNo, ReasonTrackMismatch},
		{"ghcr.io/msch128/mnema-talk:0", "0.4.0", ReachYes, ReasonFollows},
		{"ghcr.io/msch128/mnema-talk:1", "0.4.0", ReachNo, ReasonTrackMismatch},
		{"ghcr.io/msch128/mnema-talk:0.3.0", "0.4.0", ReachNo, ReasonPinned},
		{"ghcr.io/msch128/mnema-talk@sha256:abc", "0.4.0", ReachNo, ReasonDigest},
		{"ghcr.io/msch128/mnema-talk:edge", "0.4.0", ReachUnknown, ReasonCustomTag},
		{"localhost:5000/mnema-talk:0.4", "0.4.1", ReachYes, ReasonFollows},
	} {
		reach, reason := ImageReach(tc.image, tc.latest)
		if reach != tc.reach || reason != tc.reason {
			t.Errorf("ImageReach(%q, %q) = %s/%s, want %s/%s", tc.image, tc.latest, reach, reason, tc.reach, tc.reason)
		}
	}
}
