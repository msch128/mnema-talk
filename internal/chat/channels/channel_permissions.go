package channels

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/events/wire"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// VisibleHierarchy drops the channels the caller may not view, and the
// categories that are hidden from them and have nothing left in them.
func VisibleHierarchy(perms *perm.Service, r *http.Request, cats []core.Category, uncat []core.Channel) ([]core.Category, []core.Channel, error) {
	ctx := r.Context()
	snap, err := perms.Snapshot(ctx)
	if err != nil {
		return nil, nil, err
	}
	v := &hierarchyView{perms: perms, ctx: ctx, sub: auth.UserFrom(ctx).Subject(), snap: snap}
	outCats := make([]core.Category, 0, len(cats))
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

func (v *hierarchyView) canView(ch core.Channel) (bool, error) {
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
func (v *hierarchyView) viewable(in []core.Channel) ([]core.Channel, error) {
	out := make([]core.Channel, 0, len(in))
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

// CheckWholeCategory is the check for deleting a category on a state read
// under the permission lock: the caller must see the category and every
// channel in it, and when it carries overwrites (deleting it copies them into
// its channels) must be an Administrator or the Owner.
func CheckWholeCategory(snap *perm.Snapshot, sub perm.Subject, id uuid.UUID) error {
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

// SeenLayout prepares a layout change on a state read under the permission
// lock: every category and channel it names that the caller cannot see is
// replaced by a fresh random id, and the moves are checked for the caller.
//
// A hidden object thus takes exactly the path of one that does not exist: the
// same checks, in the same order, with the same database work, and the answer
// (the request's ids are never echoed) cannot tell the two apart, whatever
// else is wrong with the request. Apply the returned request, not the given one.
func SeenLayout(snap *perm.Snapshot, sub perm.Subject, req LayoutRequest) (LayoutRequest, error) {
	standIns := map[uuid.UUID]uuid.UUID{} // one per hidden id, so a repeated id stays repeated
	standIn := func(id uuid.UUID) uuid.UUID {
		if f, ok := standIns[id]; ok {
			return f
		}
		f := uuid.New()
		standIns[id] = f
		return f
	}
	seenCategory := func(id uuid.UUID) uuid.UUID {
		if snap.KnowsCategory(id) && !snap.CategoryVisible(sub, id) {
			return standIn(id)
		}
		return id
	}
	out := LayoutRequest{
		Categories: make([]CategoryOrder, len(req.Categories)),
		Channels:   make([]ChannelPlacement, len(req.Channels)),
	}
	for i, c := range req.Categories {
		c.ID = seenCategory(c.ID)
		out.Categories[i] = c
	}
	for i, c := range req.Channels {
		if !snap.Channel(sub, c.ID).Has(perm.ViewChannel) {
			c.ID = standIn(c.ID)
		}
		if c.CategoryID != nil {
			to := seenCategory(*c.CategoryID)
			c.CategoryID = &to
		}
		out.Channels[i] = c
	}
	for _, c := range out.Channels {
		if err := checkChannelMove(snap, sub, c); err != nil {
			return LayoutRequest{}, err
		}
	}
	return out, nil
}

func categoryOverwritesAffected(snap *perm.Snapshot, from, to uuid.UUID) bool {
	if from == to {
		return false
	}
	return snap.HasCategoryOverwrites(from) || snap.HasCategoryOverwrites(to)
}

// checkChannelMove checks one placement of a layout change: the caller sees
// the channel (one they cannot see was replaced by an unknown id, which they do
// not see either) and may move it between categories.
func checkChannelMove(snap *perm.Snapshot, sub perm.Subject, c ChannelPlacement) error {
	if !snap.Channel(sub, c.ID).Has(perm.ViewChannel) {
		return perm.ErrChannelHidden
	}
	to := uuid.Nil
	if c.CategoryID != nil {
		to = *c.CategoryID
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

// ChannelsChanged tells everyone that a name, topic or limit changed: the list
// is refetched, the permission state is untouched.
func (a *API) ChannelsChanged() {
	a.Guard.Perms.Invalidate() // new categories and the like must be known
	a.Events.Broadcast(wire.ChannelsChanged{})
}

// structureChanged is channelsChanged for changes that can alter who sees what
// (a channel or category was created, deleted, duplicated or moved): the
// permission state is rebuilt and live connections are re-checked.
func (a *API) structureChanged() {
	a.Guard.Perms.Changed(nil)
	a.Events.Broadcast(wire.ChannelsChanged{})
}
