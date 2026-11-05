package core

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Guard is where chat request handlers ask the permission model. Every domain
// package holds one (built from the Perms it is wired with), so none of them
// needs the chat Handler for it. A Guard without Perms (unit tests) imposes no
// limits; the router always sets it.
type Guard struct {
	Perms *perm.Service
}

// Require checks that the caller holds need in channel ch. Without
// ViewChannel the channel does not exist for them (404).
func (g Guard) Require(r *http.Request, ch uuid.UUID, need perm.Set) error {
	if g.Perms == nil {
		return nil
	}
	return g.Perms.RequireChannel(r.Context(), auth.UserFrom(r.Context()).Subject(), ch, need)
}

// ChannelPerms is what the caller may do in channel ch.
func (g Guard) ChannelPerms(r *http.Request, ch uuid.UUID) (perm.Set, error) {
	return g.Perms.Channel(r.Context(), auth.UserFrom(r.Context()).Subject(), ch)
}

// RefusalFor turns a "missing permission" refusal into RULES_NOT_ACCEPTED when
// the caller's rules acceptance is what took the permission away.
//
// need is what the refused action required; a new member's wait for it is
// named when it is what took the permission away.
func (g Guard) RefusalFor(r *http.Request, err error, need perm.Set) error {
	var apiErr *httpx.APIError
	if g.Perms == nil || !errors.As(err, &apiErr) || apiErr.Code != httpx.CodeForbidden {
		return err
	}
	snap, serr := g.Perms.Snapshot(r.Context())
	if serr != nil {
		return err
	}
	uid := auth.UserFrom(r.Context()).ID
	if snap.RulesPending(uid) {
		return perm.ErrRulesNotAccepted
	}
	if nm := snap.NewMemberRefusal(uid, need); nm != nil {
		return nm
	}
	return err
}

// ViewableChannels lists the channels the caller may view; nil means all.
func (g Guard) ViewableChannels(r *http.Request) ([]uuid.UUID, error) {
	if g.Perms == nil {
		return nil, nil
	}
	snap, err := g.Perms.Snapshot(r.Context())
	if err != nil {
		return nil, err
	}
	return snap.ViewableChannels(auth.UserFrom(r.Context()).Subject()), nil
}

// ViewableCategories lists the categories the caller may see; nil means all.
func (g Guard) ViewableCategories(r *http.Request) ([]uuid.UUID, error) {
	if g.Perms == nil {
		return nil, nil
	}
	snap, err := g.Perms.Snapshot(r.Context())
	if err != nil {
		return nil, err
	}
	return snap.VisibleCategories(auth.UserFrom(r.Context()).Subject()), nil
}

// RequireCategory checks that the caller may see a category (a category that
// does not exist is left to the operation itself to report).
func (g Guard) RequireCategory(r *http.Request, id uuid.UUID) error {
	if g.Perms == nil {
		return nil
	}
	snap, err := g.Perms.Snapshot(r.Context())
	if err != nil {
		return err
	}
	if !snap.KnowsCategory(id) {
		return nil
	}
	if !snap.CategoryVisible(auth.UserFrom(r.Context()).Subject(), id) {
		return ErrCategoryNotFound
	}
	return nil
}

// NotFoundUnlessFailure turns a refusal (the member may not view the message's
// channel) into the message's own 404, so it looks like any missing message,
// and passes real failures (an unreadable permission state) through.
func NotFoundUnlessFailure(err error) error {
	var apiErr *httpx.APIError
	if errors.As(err, &apiErr) {
		return ErrMessageNotFound
	}
	return err
}
