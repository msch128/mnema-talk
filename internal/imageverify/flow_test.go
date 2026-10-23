package imageverify_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/updaterd"
	"github.com/msch128/mnema-talk/internal/updaterd/daemontest"
)

// These tests run the whole update path: the real updater server, the real
// Docker API client (against a fake daemon on a Unix socket), the real
// verifier (against an in-memory registry and a virtual Sigstore) and a fake
// recreator. They show what SUP-05 promises: whatever is wrong with the new
// image, the updater refuses and pulls, tags and restarts nothing.

const flowToken = "0123456789abcdef0123456789abcdef-token"

func flow(t *testing.T, f *Fixture, d *daemontest.Daemon) *updaterd.Server {
	t.Helper()
	return &updaterd.Server{
		Token:    flowToken,
		Scope:    "mnema-talk",
		Engine:   updaterd.NewDocker(d.Socket),
		Verifier: f.Verifier(),
		Recreator: &updaterd.HTTPRecreator{
			Endpoint: d.RecreatorURL(t, flowToken), Token: flowToken, Client: http.DefaultClient,
		},
		Log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		Background: func(fn func()) { fn() }, // run the swap inline
	}
}

func update(s *updaterd.Server) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/update?async=true&target_version=1.1.0", nil)
	req.Header.Set("Authorization", "Bearer "+flowToken)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func TestUpdateOfASignedImageIsPulledByDigestTaggedAndRecreated(t *testing.T) {
	f := newFixture(t)
	rel := f.Push("1.0")
	f.Sign(rel, Identity, rel.Digest)
	d := daemontest.New(t, f.Repo+":1.0")

	if rec := update(flow(t, f, d)); rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
	}
	want := []string{
		"list labels=com.centurylinklabs.watchtower.enable=true,com.centurylinklabs.watchtower.scope=mnema-talk",
		"pull " + f.Repo + "@" + rel.Digest.String(),
		"tag " + f.Repo + "@" + rel.Digest.String() + " -> " + f.Repo + ":1.0",
		"recreate /v1/update auth=true",
	}
	if got := d.Log(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("daemon calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestUpdateOfAnUntrustedImageChangesNothing(t *testing.T) {
	cases := map[string]struct {
		prepare func(f *Fixture)
		status  int
		reason  string
	}{
		"unsigned image": {
			prepare: func(f *Fixture) { f.Push("1.0") },
			status:  http.StatusUnprocessableEntity, reason: "verification_failed",
		},
		"signature of another workflow": {
			prepare: func(f *Fixture) {
				rel := f.Push("1.0")
				f.Sign(rel, "https://github.com/msch128/mnema-talk/.github/workflows/ci.yml@refs/heads/main", rel.Digest)
			},
			status: http.StatusUnprocessableEntity, reason: "verification_failed",
		},
		"tampered image under a copied signature": {
			prepare: func(f *Fixture) {
				good := f.Push("signed")
				f.Sign(good, Identity, good.Digest)
				evil := f.Push("1.0")
				f.Sign(evil, Identity, good.Digest)
			},
			status: http.StatusUnprocessableEntity, reason: "verification_failed",
		},
		"forged provenance": {
			prepare: func(f *Fixture) {
				rel := f.Push("1.0", Provenance{Source: OtherSource})
				f.Sign(rel, Identity, rel.Digest)
			},
			status: http.StatusUnprocessableEntity, reason: "verification_failed",
		},
		"tag the registry does not know": {
			prepare: func(f *Fixture) {},
			status:  http.StatusBadGateway, reason: "verification_unavailable",
		},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			f := newFixture(t)
			tc.prepare(f)
			d := daemontest.New(t, f.Repo+":1.0")

			rec := update(flow(t, f, d))
			if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.reason) {
				t.Fatalf("status = %d body = %s, want %d %s", rec.Code, rec.Body, tc.status, tc.reason)
			}
			for _, c := range d.Log() {
				if !strings.HasPrefix(c, "list ") {
					t.Fatalf("a refused update must only have looked: %q", d.Log())
				}
			}
		})
	}
}

// A valid signature says who built an image, not which release it is.
func TestUpdateRefusesTheWrongRelease(t *testing.T) {
	cases := map[string]struct {
		signedVersion, running, target string
	}{
		"validly signed older release":     {"1.0.0", "1.0.1", "1.1.0"},
		"signed release is not the target": {"1.2.0", "1.0.1", "1.1.0"},
		"not newer than the running one":   {"1.1.0", "1.1.0", "1.1.0"},
		"downgrade even if requested":      {"1.0.0", "1.0.1", "1.0.0"},
		"running version unknown":          {"1.1.0", "dev", "1.1.0"},
		"no release version in provenance": {"-", "1.0.1", "1.1.0"},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			f := newFixture(t)
			rel := f.Push("1.0", Provenance{Version: tc.signedVersion}, Provenance{Version: tc.signedVersion})
			f.Sign(rel, Identity, rel.Digest)
			d := daemontest.New(t, f.Repo+":1.0")
			d.Version = tc.running

			req := httptest.NewRequest(http.MethodPost, "/v1/update?async=true&target_version="+tc.target, nil)
			req.Header.Set("Authorization", "Bearer "+flowToken)
			rec := httptest.NewRecorder()
			flow(t, f, d).ServeHTTP(rec, req)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d (%s), want a refusal", rec.Code, rec.Body)
			}
			for _, c := range d.Log() {
				if !strings.HasPrefix(c, "list ") {
					t.Fatalf("a refused update must only have looked: %q", d.Log())
				}
			}
		})
	}
}

func TestUpdateNeedsAValidTargetVersion(t *testing.T) {
	f := newFixture(t)
	rel := f.Push("1.0")
	f.Sign(rel, Identity, rel.Digest)
	d := daemontest.New(t, f.Repo+":1.0")
	for _, q := range []string{"", "&target_version=", "&target_version=latest", "&target_version=1.1"} {
		req := httptest.NewRequest(http.MethodPost, "/v1/update?async=true"+q, nil)
		req.Header.Set("Authorization", "Bearer "+flowToken)
		rec := httptest.NewRecorder()
		flow(t, f, d).ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%q: status = %d, want 400", q, rec.Code)
		}
	}
	if len(d.Log()) != 0 {
		t.Fatalf("calls = %q", d.Log())
	}
}

func TestUpdateRefusesImagesOfAnotherRepository(t *testing.T) {
	f := newFixture(t)
	rel := f.Push("1.0")
	f.Sign(rel, Identity, rel.Digest)
	d := daemontest.New(t, f.Host+"/other/app:1.0") // a labelled container with a foreign image

	if rec := update(flow(t, f, d)); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want a refusal", rec.Code)
	}
	if len(d.Log()) != 1 {
		t.Fatalf("calls = %q", d.Log())
	}
}

func TestUpdateStopsBeforeTheRestartWhenThePullFails(t *testing.T) {
	f := newFixture(t)
	rel := f.Push("1.0")
	f.Sign(rel, Identity, rel.Digest)
	d := daemontest.New(t, f.Repo+":1.0")
	d.PullFail = "manifest unknown"

	if rec := update(flow(t, f, d)); rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d", rec.Code)
	}
	for _, c := range d.Log() {
		if strings.HasPrefix(c, "tag ") || strings.HasPrefix(c, "recreate") {
			t.Fatalf("a failed pull must not lead to a restart: %q", d.Log())
		}
	}
}
