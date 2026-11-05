package updaterd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/imageverify"
)

// versionVerifier reports a validly signed image of the given release.
type versionVerifier struct{ version string }

func (v versionVerifier) Verify(context.Context, string) (*imageverify.Result, error) {
	return &imageverify.Result{Repository: "r/x", Digest: "sha256:" + strings.Repeat("b", 64), Version: v.version}, nil
}

// A valid signature says who built an image, not which release it is: the
// updater must refuse a signed image that is older than, equal to or other
// than the release the admin asked for, before it pulls or restarts anything.
func TestUpdateRefusesASignedImageThatIsNotTheNewerRequestedRelease(t *testing.T) {
	cases := map[string]struct {
		running, signed, query string
		want                   int
	}{
		"downgrade to an older signed release": {"2.0.0", "1.9.0", "1.9.0", http.StatusUnprocessableEntity},
		"same release as running":              {"2.0.0", "2.0.0", "2.0.0", http.StatusUnprocessableEntity},
		"signed release is not the requested":  {"1.0.0", "1.5.0", "2.0.0", http.StatusUnprocessableEntity},
		"signed version is not a release":      {"1.0.0", "dev", "2.0.0", http.StatusUnprocessableEntity},
		"running version unknown":              {"", "2.0.0", "2.0.0", http.StatusUnprocessableEntity},
		"newer requested release":              {"1.0.0", "2.0.0", "v2.0.0", http.StatusAccepted},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			eng := &recordingEngine{stubEngine: stubEngine{targets: []Target{{Name: "app", Image: "r/x:1", RunningVersion: c.running}}}}
			rc := &countingRecreator{}
			srv := &Server{Token: token, Engine: eng, Verifier: versionVerifier{c.signed}, Recreator: rc, Log: quiet(), Background: func(f func()) { f() }}
			req := httptest.NewRequest(http.MethodPost, "/v1/update?target_version="+c.query, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Fatalf("status = %d, want %d", rec.Code, c.want)
			}
			if c.want != http.StatusAccepted && (len(eng.calls) != 0 || rc.n != 0 || srv.busy) {
				t.Fatalf("a refusal must change nothing and free the slot (calls %v, restarts %d, busy %v)", eng.calls, rc.n, srv.busy)
			}
			if c.want == http.StatusUnprocessableEntity && !strings.Contains(rec.Body.String(), reasonVersion) {
				t.Fatalf("body %q does not carry %q", rec.Body.String(), reasonVersion)
			}
		})
	}
}

func TestUpdateNeedsAValidTargetVersion(t *testing.T) {
	eng := &recordingEngine{stubEngine: stubEngine{targets: []Target{{Name: "app", Image: "r/x:1", RunningVersion: "1.0.0"}}}}
	srv := &Server{Token: token, Engine: eng, Verifier: okVerifier{}, Recreator: &countingRecreator{}, Log: quiet()}
	for _, q := range []string{"", "?target_version=", "?target_version=latest", "?target_version=2.0"} {
		req := httptest.NewRequest(http.MethodPost, "/v1/update"+q, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest || len(eng.calls) != 0 || srv.busy {
			t.Errorf("%q: status %d, calls %v, busy %v; want 400 and nothing touched", q, rec.Code, eng.calls, srv.busy)
		}
	}
}

// failingPull makes the verified image's pull fail.
type failingPull struct{ recordingEngine }

func (e *failingPull) Pull(context.Context, string, string) error { return context.DeadlineExceeded }

func TestSwapDoesNotTagOrRestartWhenThePullFails(t *testing.T) {
	eng := &failingPull{recordingEngine{stubEngine: stubEngine{targets: []Target{{Name: "app", Image: "r/x:1", RunningVersion: "1.0.0"}}}}}
	rc := &countingRecreator{}
	srv := &Server{Token: token, Engine: eng, Verifier: okVerifier{}, Recreator: rc, Log: quiet(), Background: func(f func()) { f() }}
	if rec := post(srv, "Bearer "+token); rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d", rec.Code)
	}
	if rc.n != 0 || len(eng.calls) != 0 {
		t.Fatalf("a failed pull must not tag or restart (calls %v, restarts %d)", eng.calls, rc.n)
	}
	if srv.busy {
		t.Fatal("the slot stays taken after a failed swap")
	}
}
