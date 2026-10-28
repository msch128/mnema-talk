package updaterd

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/msch128/mnema-talk/internal/imageverify"
)

const token = "0123456789abcdef0123456789abcdef-token"

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func post(s *Server, auth string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/update?async=true&target_version=2.0.0", nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

// The end-to-end behaviour with real signatures (accepted, unsigned, tampered,
// forged provenance) is tested in internal/imageverify, which has the fixture.

func TestUpdateRequiresTheTokenAndOneAtATime(t *testing.T) {
	eng := &recordingEngine{stubEngine: stubEngine{targets: []Target{{Name: "app", Image: "r/x:1", RunningVersion: "1.0.0"}}}}
	srv := &Server{Token: token, Scope: "s", Engine: eng, Verifier: okVerifier{}, Recreator: &countingRecreator{}, Log: quiet()}

	for _, auth := range []string{"", "Bearer wrong", "Basic " + token, token} {
		if rec := post(srv, auth); rec.Code != http.StatusUnauthorized {
			t.Errorf("auth %q: status = %d, want 401", auth, rec.Code)
		}
	}
	if len(eng.calls) != 0 {
		t.Fatalf("an unauthorised call must not touch Docker: %q", eng.calls)
	}

	srv.busy = true
	if rec := post(srv, "Bearer "+token); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("busy: status = %d, want 429", rec.Code)
	}
	if !srv.busy {
		t.Fatal("a rejected call must not free the running update's slot")
	}
}

func TestUpdateWithoutAContainerInScope(t *testing.T) {
	srv := &Server{Token: token, Scope: "mnema-talk", Log: quiet(), Engine: stubEngine{}, Verifier: failVerifier{}}
	if rec := post(srv, "Bearer "+token); rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	srv.Engine = stubEngine{err: context.DeadlineExceeded}
	if rec := post(srv, "Bearer "+token); rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}

func TestAnUnverifiableImageRefusesTheWholeUpdate(t *testing.T) {
	for verdict, want := range map[error]int{
		imageverify.ErrUntrusted: http.StatusUnprocessableEntity,
		context.DeadlineExceeded: http.StatusBadGateway,
	} {
		eng := &recordingEngine{stubEngine: stubEngine{targets: []Target{{Name: "a", Image: "r/x:1"}, {Name: "b", Image: "r/y:1"}}}}
		srv := &Server{Token: token, Engine: eng, Verifier: errVerifier{verdict}, Recreator: &countingRecreator{}, Log: quiet()}
		if rec := post(srv, "Bearer "+token); rec.Code != want {
			t.Errorf("%v: status = %d, want %d", verdict, rec.Code, want)
		}
		if len(eng.calls) != 0 || srv.busy {
			t.Errorf("%v: a refusal must change nothing and free the slot (calls %v, busy %v)", verdict, eng.calls, srv.busy)
		}
	}
}

func TestHealthzAndUnknownRoutes(t *testing.T) {
	srv := &Server{Token: token}
	for path, want := range map[string]int{"/healthz": http.StatusNoContent, "/v1/other": http.StatusNotFound} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != want {
			t.Errorf("%s: status = %d, want %d", path, rec.Code, want)
		}
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/update", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /v1/update: status = %d", rec.Code)
	}
}

func TestConfig(t *testing.T) {
	env := map[string]string{"UPDATER_TOKEN": token}
	get := func(k string) string { return env[k] }
	cfg, err := ParseConfig(get)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Policy != imageverify.DefaultPolicy() || cfg.Scope != "mnema-talk" || cfg.RecreatorURL != "http://mnema-recreator:8080" {
		t.Fatalf("defaults = %+v", cfg)
	}
	if _, err := NewServer(cfg, slog.Default()); err != nil {
		t.Fatal(err)
	}
	cfg.TrustedRoot = "/nonexistent/trusted_root.json"
	if _, err := NewServer(cfg, slog.Default()); err != nil {
		t.Fatalf("a trusted root file is read lazily, on the first verification: %v", err)
	}

	for label, kv := range map[string][2]string{
		"short token":       {"UPDATER_TOKEN", "short"},
		"unanchored policy": {"UPDATER_CERT_IDENTITY_REGEXP", "release.yml"},
		"bad recreator":     {"UPDATER_RECREATOR_URL", "ftp://x"},
	} {
		bad := map[string]string{"UPDATER_TOKEN": token}
		bad[kv[0]] = kv[1]
		if _, err := ParseConfig(func(k string) string { return bad[k] }); err == nil {
			t.Errorf("%s accepted", label)
		}
	}
	if code := Run(func(string) string { return "" }, io.Discard); code != 2 {
		t.Errorf("Run without a token exited %d, want 2", code)
	}
}

type stubEngine struct {
	targets []Target
	err     error
}

func (s stubEngine) Targets(context.Context, string) ([]Target, error) { return s.targets, s.err }
func (stubEngine) Pull(context.Context, string, string) error          { return nil }
func (stubEngine) Tag(context.Context, string, string, string) error   { return nil }

type failVerifier struct{}

func (failVerifier) Verify(context.Context, string) (*imageverify.Result, error) {
	return nil, imageverify.ErrUntrusted
}

type errVerifier struct{ err error }

func (e errVerifier) Verify(context.Context, string) (*imageverify.Result, error) { return nil, e.err }
