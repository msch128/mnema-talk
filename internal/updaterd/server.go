// Package updaterd is the self-update sidecar (cmd/updater). It sits between
// the app and the recreator (Watchtower): on "update now" it verifies the
// signature and provenance of the image the app's tag points to, and only then
// pulls exactly that digest, moves the local tag onto it and lets the
// recreator restart the app from the local image. A failed check changes
// nothing.
package updaterd

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/msch128/mnema-talk/internal/imageverify"
	"github.com/msch128/mnema-talk/internal/update"
)

// ImageVerifier is what the updater needs from imageverify.
type ImageVerifier interface {
	Verify(ctx context.Context, image string) (*imageverify.Result, error)
}

// Recreator restarts the containers in scope from their (local) image.
type Recreator interface {
	Recreate(ctx context.Context) error
}

// Refusal reasons in the 4xx/5xx body: stable strings shared with the app's
// client (update.Reason*), which maps them to its own wording.
const (
	reasonUntrusted = update.ReasonUntrusted
	reasonUnchecked = update.ReasonUnchecked
	reasonNoTarget  = update.ReasonNoTarget
	reasonVersion   = update.ReasonVersion
	reasonRequest   = update.ReasonRequest
	reasonEngine    = update.ReasonEngine
)

const (
	verifyTimeout = 90 * time.Second
	swapTimeout   = 15 * time.Minute
	// swapGrace is how long a shutdown waits for a running swap.
	swapGrace = 2 * time.Minute
)

// Server is the updater's HTTP handler.
type Server struct {
	Token     string
	Scope     string
	Engine    Engine
	Verifier  ImageVerifier
	Recreator Recreator
	Log       *slog.Logger

	// Background runs the swap after the 202; tests replace it to wait.
	Background func(func())

	mu    sync.Mutex
	busy  bool
	swaps sync.WaitGroup
}

// ServeHTTP serves POST /v1/update (Bearer token) and GET /healthz.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/healthz" && r.Method == http.MethodGet:
		w.WriteHeader(http.StatusNoContent)
	case r.URL.Path == "/v1/update" && r.Method == http.MethodPost:
		s.handleUpdate(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) authorized(r *http.Request) bool {
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && s.Token != "" && subtle.ConstantTimeCompare([]byte(got), []byte(s.Token)) == 1
}

func reply(w http.ResponseWriter, status int, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if reason != "" {
		_ = json.NewEncoder(w).Encode(map[string]string{"error": reason})
	}
}

// verified is one image the update will switch to.
type verified struct {
	target Target
	result *imageverify.Result
	tag    string
}

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		reply(w, http.StatusUnauthorized, "")
		return
	}
	want, ok := update.ParseSemver(strings.TrimPrefix(r.URL.Query().Get("target_version"), "v"))
	if !ok {
		reply(w, http.StatusBadRequest, reasonRequest)
		return
	}
	if !s.acquire() {
		reply(w, http.StatusTooManyRequests, "")
		return
	}
	handedOver := false
	defer func() {
		if !handedOver {
			s.release()
		}
	}()

	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), verifyTimeout)
	defer cancel()
	targets, err := s.Engine.Targets(ctx, s.Scope)
	if err != nil {
		s.Log.Error("update refused: cannot list containers", "err", err)
		reply(w, http.StatusBadGateway, reasonEngine)
		return
	}
	if len(targets) == 0 {
		s.Log.Warn("update refused: no running container in scope", "scope", s.Scope)
		reply(w, http.StatusConflict, reasonNoTarget)
		return
	}
	plan, status, reason := s.verifyAll(ctx, targets, want)
	if reason != "" {
		reply(w, status, reason)
		return
	}
	handedOver = true
	reply(w, http.StatusAccepted, "")
	s.background(func() {
		defer s.release()
		s.swap(plan)
	})
}

// verifyAll checks every target before anything is changed: one failure
// refuses the whole update.
func (s *Server) verifyAll(ctx context.Context, targets []Target, want update.Semver) ([]verified, int, string) {
	plan := make([]verified, 0, len(targets))
	for _, t := range targets {
		res, err := s.Verifier.Verify(ctx, t.Image)
		if err != nil {
			s.Log.Error("UPDATE REFUSED: the image did not pass verification; nothing was pulled or restarted",
				"container", t.Name, "image", t.Image, "err", err)
			if errors.Is(err, imageverify.ErrUntrusted) {
				return nil, http.StatusUnprocessableEntity, reasonUntrusted
			}
			return nil, http.StatusBadGateway, reasonUnchecked
		}
		if err := checkVersion(res.Version, t.RunningVersion, want); err != nil {
			s.Log.Error("UPDATE REFUSED: the signed image is not the release that was asked for; nothing was pulled or restarted",
				"container", t.Name, "image", res.Ref(), "signed_version", res.Version, "running_version", t.RunningVersion, "target_version", want.String(), "err", err)
			return nil, http.StatusUnprocessableEntity, reasonVersion
		}
		_, tag, _ := update.SplitImage(t.Image)
		plan = append(plan, verified{target: t, result: res, tag: tag})
		s.Log.Info("image verified", "container", t.Name, "image", res.Ref(), "platforms", res.Platforms)
	}
	return plan, 0, ""
}

// swap pulls the verified digests, moves the local tags onto them and asks the
// recreator to restart. It runs after the 202; failures are logged and leave
// the app running its old container (the recreator is not called unless every
// pull and tag worked).
func (s *Server) swap(plan []verified) {
	ctx, cancel := context.WithTimeout(context.Background(), swapTimeout)
	defer cancel()
	for _, v := range plan {
		if err := s.Engine.Pull(ctx, v.result.Repository, v.result.Digest); err != nil {
			s.Log.Error("update aborted: pull of the verified image failed", "image", v.result.Ref(), "err", err)
			return
		}
		if err := s.Engine.Tag(ctx, v.result.Repository, v.result.Digest, v.tag); err != nil {
			s.Log.Error("update aborted: tagging the verified image failed", "image", v.result.Ref(), "err", err)
			return
		}
	}
	if err := s.Recreator.Recreate(ctx); err != nil {
		s.Log.Error("update failed: the recreator reported an error", "err", err)
		return
	}
	s.Log.Info("update done", "images", len(plan))
}

// checkVersion guards against rollback: a valid signature says who built an
// image, not which release it is, so an attacker who controls the tag could
// offer an older, vulnerable signed release. The version the signed
// provenance records must be the release the admin asked for and strictly
// newer than the one that runs now; anything unknown is refused.
func checkVersion(signed, running string, want update.Semver) error {
	got, ok := update.ParseSemver(strings.TrimPrefix(signed, "v"))
	if !ok {
		return fmt.Errorf("the signed version %q is not a release version", signed)
	}
	if got.Compare(want) != 0 {
		return fmt.Errorf("the image is release %s, not the requested %s", got, want)
	}
	cur, ok := update.ParseSemver(strings.TrimPrefix(running, "v"))
	if !ok {
		return fmt.Errorf("the running version %q is unknown", running)
	}
	if got.Compare(cur) <= 0 {
		return fmt.Errorf("release %s is not newer than the running %s", got, cur)
	}
	return nil
}

func (s *Server) background(f func()) {
	s.swaps.Add(1)
	run := func() {
		defer s.swaps.Done()
		f()
	}
	if s.Background != nil {
		s.Background(run)
		return
	}
	go run()
}

// WaitIdle blocks until no swap is running, or until timeout; it reports
// whether the updater is idle.
func (s *Server) WaitIdle(timeout time.Duration) bool {
	idle := make(chan struct{})
	go func() {
		s.swaps.Wait()
		close(idle)
	}()
	select {
	case <-idle:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (s *Server) acquire() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy {
		return false
	}
	s.busy = true
	return true
}

func (s *Server) release() {
	s.mu.Lock()
	s.busy = false
	s.mu.Unlock()
}
