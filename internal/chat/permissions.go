package chat

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// This file is where the chat handlers ask the permission model. A Handler
// without Perms (unit tests) imposes no limits; the router always sets it.

// require checks that the caller holds need in channel ch. Without
// ViewChannel the channel does not exist for them (404).
func (h *Handler) require(r *http.Request, ch uuid.UUID, need perm.Set) error {
	if h.Perms == nil {
		return nil
	}
	return h.Perms.RequireChannel(r.Context(), auth.UserFrom(r.Context()).Subject(), ch, need)
}

// notFoundUnlessFailure turns a refusal (the member may not view the message's
// channel) into the message's own 404, so it looks like any missing message,
// and passes real failures (an unreadable permission state) through.
func notFoundUnlessFailure(err error) error {
	if _, refused := httpx.AsAPIError(err); refused {
		return errMessageNotFound
	}
	return err
}

// channelPerms is what the caller may do in channel ch.
func (h *Handler) channelPerms(r *http.Request, ch uuid.UUID) (perm.Set, error) {
	return h.Perms.Channel(r.Context(), auth.UserFrom(r.Context()).Subject(), ch)
}

// sendContext checks need for the caller in ch and returns the request
// context carrying the mention policy that fits their permissions: @all and
// @here only notify when they may mention everyone, and nobody is notified
// about a message in a channel they cannot view.
func (h *Handler) sendContext(r *http.Request, ch uuid.UUID, need perm.Set) (context.Context, error) {
	return sendContext(h.Perms, r, ch, need)
}

func sendContext(perms *perm.Service, r *http.Request, ch uuid.UUID, need perm.Set) (context.Context, error) {
	ctx := r.Context()
	if perms == nil {
		return ctx, nil
	}
	snap, err := perms.ForChannel(ctx, ch)
	if err != nil {
		return nil, err
	}
	p := snap.Channel(auth.UserFrom(ctx).Subject(), ch)
	if err := perm.CheckChannel(p, need); err != nil {
		return nil, err
	}
	return WithMentionPolicy(ctx, MentionPolicy{
		NoEveryone: !p.Has(perm.MentionEveryone),
		CanView:    func(id uuid.UUID) bool { return snap.Channel(perm.Subject{ID: id}, ch).Has(perm.ViewChannel) },
	}), nil
}

// UploadContext is the sendContext of an upload (media package): the caller
// needs to view the channel, send messages and attach files.
func UploadContext(perms *perm.Service, r *http.Request, ch uuid.UUID) (context.Context, error) {
	return sendContext(perms, r, ch, perm.SendMessages|perm.AttachFiles)
}

// viewableChannels lists the channels the caller may view; nil means all.
func (h *Handler) viewableChannels(r *http.Request) ([]uuid.UUID, error) {
	if h.Perms == nil {
		return nil, nil
	}
	snap, err := h.Perms.Snapshot(r.Context())
	if err != nil {
		return nil, err
	}
	return snap.ViewableChannels(auth.UserFrom(r.Context()).Subject()), nil
}

// visibleHierarchy drops the channels the caller may not view, and the
// categories that are hidden from them and have nothing left in them.
func (h *Handler) visibleHierarchy(r *http.Request, cats []Category, uncat []Channel) ([]Category, []Channel, error) {
	if h.Perms == nil {
		return cats, uncat, nil
	}
	ctx := r.Context()
	sub := auth.UserFrom(ctx).Subject()
	snap, err := h.Perms.Snapshot(ctx)
	if err != nil {
		return nil, nil, err
	}
	view := func(ch Channel) (bool, error) {
		if !snap.KnowsChannel(ch.ID) {
			if snap, err = h.Perms.ForChannel(ctx, ch.ID); err != nil {
				return false, err
			}
		}
		return snap.Channel(sub, ch.ID).Has(perm.ViewChannel), nil
	}
	filter := func(in []Channel) ([]Channel, error) {
		out := make([]Channel, 0, len(in))
		for _, ch := range in {
			ok, err := view(ch)
			if err != nil {
				return nil, err
			}
			if ok {
				out = append(out, ch)
			}
		}
		return out, nil
	}
	outCats := make([]Category, 0, len(cats))
	for _, cat := range cats {
		chans, err := filter(cat.Channels)
		if err != nil {
			return nil, nil, err
		}
		if len(chans) == 0 && !snap.CategoryVisible(sub, cat.ID) {
			continue
		}
		cat.Channels = chans
		outCats = append(outCats, cat)
	}
	outUncat, err := filter(uncat)
	if err != nil {
		return nil, nil, err
	}
	return outCats, outUncat, nil
}

// visibleReadStates keeps the read states of viewable channels.
func (h *Handler) visibleReadStates(r *http.Request, states []ReadState) ([]ReadState, error) {
	visible, err := h.viewableChannels(r)
	if err != nil || visible == nil {
		return states, err
	}
	ok := make(map[uuid.UUID]bool, len(visible))
	for _, id := range visible {
		ok[id] = true
	}
	out := make([]ReadState, 0, len(states))
	for _, s := range states {
		if ok[s.ChannelID] {
			out = append(out, s)
		}
	}
	return out, nil
}

// requireCategory checks that the caller may see a category (a category that
// does not exist is left to the operation itself to report).
func (h *Handler) requireCategory(r *http.Request, id uuid.UUID) error {
	if h.Perms == nil {
		return nil
	}
	snap, err := h.Perms.Snapshot(r.Context())
	if err != nil {
		return err
	}
	if !snap.KnowsCategory(id) {
		return nil
	}
	if !snap.CategoryVisible(auth.UserFrom(r.Context()).Subject(), id) {
		return errCategoryNotFound
	}
	return nil
}

// requireLayout checks that the caller sees everything a layout change names.
func (h *Handler) requireLayout(r *http.Request, req LayoutRequest) error {
	for _, c := range req.Categories {
		if err := h.requireCategory(r, c.ID); err != nil {
			return err
		}
	}
	for _, c := range req.Channels {
		if err := h.require(r, c.ID, perm.ViewChannel); err != nil {
			return err
		}
		if c.CategoryID != nil {
			if err := h.requireCategory(r, *c.CategoryID); err != nil {
				return err
			}
		}
	}
	return nil
}

// channelsChanged tells everyone that categories or channels changed. They
// can also change who sees what (a channel moved into a private category), so
// the permission state is rebuilt and live connections are re-checked.
func (h *Handler) channelsChanged() {
	h.Perms.Changed(nil)
	h.Events.Broadcast("channels_changed", nil)
}
