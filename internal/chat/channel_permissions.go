package chat

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// visibleHierarchy drops the channels the caller may not view, and the
// categories that are hidden from them and have nothing left in them.
func visibleHierarchy(perms *perm.Service, r *http.Request, cats []Category, uncat []Channel) ([]Category, []Channel, error) {
	if perms == nil {
		return cats, uncat, nil
	}
	ctx := r.Context()
	snap, err := perms.Snapshot(ctx)
	if err != nil {
		return nil, nil, err
	}
	v := &hierarchyView{perms: perms, ctx: ctx, sub: auth.UserFrom(ctx).Subject(), snap: snap}
	outCats := make([]Category, 0, len(cats))
	for _, cat := range cats {
		chans, err := v.viewable(cat.Channels)
		if err != nil {
			return nil, nil, err
		}
		if len(chans) == 0 && !v.snap.CategoryVisible(v.sub, cat.ID) {
			continue
		}
		cat.Channels = chans
		outCats = append(outCats, cat)
	}
	outUncat, err := v.viewable(uncat)
	if err != nil {
		return nil, nil, err
	}
	return outCats, outUncat, nil
}

// hierarchyView answers "may the caller view this channel" against the
// permission snapshot, loading a fresher one for a channel it does not know.
type hierarchyView struct {
	perms *perm.Service
	ctx   context.Context
	sub   perm.Subject
	snap  *perm.Snapshot
}

func (v *hierarchyView) canView(ch Channel) (bool, error) {
	if !v.snap.KnowsChannel(ch.ID) {
		snap, err := v.perms.ForChannel(v.ctx, ch.ID)
		if err != nil {
			return false, err
		}
		v.snap = snap
	}
	return v.snap.Channel(v.sub, ch.ID).Has(perm.ViewChannel), nil
}

// viewable returns the channels of in the caller may view.
func (v *hierarchyView) viewable(in []Channel) ([]Channel, error) {
	out := make([]Channel, 0, len(in))
	for _, ch := range in {
		ok, err := v.canView(ch)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, ch)
		}
	}
	return out, nil
}

// checkWholeCategory is the check for deleting a category on a state read
// under the permission lock: the caller must see the category and every
// channel in it, and when it carries overwrites (deleting it copies them into
// its channels) must be an Administrator or the Owner.
func checkWholeCategory(snap *perm.Snapshot, sub perm.Subject, id uuid.UUID) error {
	if !snap.KnowsCategory(id) {
		return nil // the deletion itself reports a missing category
	}
	if !snap.CategoryVisible(sub, id) || !snap.CanSeeAllChannelsIn(sub, id) {
		return core.ErrCategoryNotFound
	}
	if snap.HasCategoryOverwrites(id) && !snap.Global(sub).Has(perm.Administrator) {
		return httpx.ErrForbidden("deleting a category with permissions needs the Administrator permission")
	}
	return nil
}

// checkLayout checks, on a state read under the permission lock, that the
// caller sees everything a layout change names and may make its moves.
func checkLayout(snap *perm.Snapshot, sub perm.Subject, req LayoutRequest) error {
	for _, c := range req.Categories {
		if snap.KnowsCategory(c.ID) && !snap.CategoryVisible(sub, c.ID) {
			return core.ErrCategoryNotFound
		}
	}
	for _, c := range req.Channels {
		if err := checkChannelMove(snap, sub, c); err != nil {
			return err
		}
	}
	return nil
}

func targetCategoryVisible(snap *perm.Snapshot, sub perm.Subject, target *uuid.UUID) (uuid.UUID, error) {
	if target == nil {
		return uuid.Nil, nil
	}
	to := *target
	if snap.KnowsCategory(to) && !snap.CategoryVisible(sub, to) {
		return uuid.Nil, core.ErrCategoryNotFound
	}
	return to, nil
}

func categoryOverwritesAffected(snap *perm.Snapshot, from, to uuid.UUID) bool {
	if from == to {
		return false
	}
	return snap.HasCategoryOverwrites(from) || snap.HasCategoryOverwrites(to)
}

// checkChannelMove checks one placement of a layout change: the caller sees
// the channel and its target category, and may move it between categories.
func checkChannelMove(snap *perm.Snapshot, sub perm.Subject, c ChannelPlacement) error {
	if !snap.Channel(sub, c.ID).Has(perm.ViewChannel) {
		return perm.ErrChannelHidden
	}
	to, err := targetCategoryVisible(snap, sub, c.CategoryID)
	if err != nil {
		return err
	}
	// Moving a channel takes on the permissions of its new category and
	// drops those of the old one, which could undo a restriction the
	// caller may not lift: with overwrites on either side, only an
	// Administrator (or the Owner) moves it.
	from, inCategory := snap.CategoryOf(c.ID)
	if !inCategory {
		from = uuid.Nil
	}
	if categoryOverwritesAffected(snap, from, to) && !snap.Global(sub).Has(perm.Administrator) {
		return httpx.ErrForbidden("moving a channel between categories with permissions needs the Administrator permission")
	}
	return nil
}

// channelsChanged tells everyone that a name, topic or limit changed: the list
// is refetched, the permission state is untouched.
func (h *Handler) channelsChanged() {
	h.Perms.Invalidate() // new categories and the like must be known
	h.Events.Broadcast(events.ChannelsChanged, nil)
}

// structureChanged is channelsChanged for changes that can alter who sees what
// (a channel or category was created, deleted, duplicated or moved): the
// permission state is rebuilt and live connections are re-checked.
func (h *Handler) structureChanged() {
	h.Perms.Changed(nil)
	h.Events.Broadcast(events.ChannelsChanged, nil)
}
