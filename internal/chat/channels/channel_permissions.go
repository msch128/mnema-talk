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

// CheckLayout checks, on a state read under the permission lock, that the
// caller sees everything a layout change names and may make its moves.
//
// A category the caller cannot see answers what applying the layout answers
// for one that does not exist, and at the same point of the checks (after the
// sort orders of its list, before the next list's): the answer does not tell
// the two apart, whatever else is wrong with the request.
func CheckLayout(snap *perm.Snapshot, sub perm.Subject, req LayoutRequest) error {
	for _, c := range req.Categories {
		if err := CheckSortOrder(c.SortOrder); err != nil {
			return err
		}
	}
	for _, c := range req.Categories {
		if snap.KnowsCategory(c.ID) && !snap.CategoryVisible(sub, c.ID) {
			return core.ErrCategoryNotFound
		}
	}
	hiddenTarget := false
	for _, c := range req.Channels {
		hidden, err := checkChannelMove(snap, sub, c)
		if err != nil {
			return err
		}
		hiddenTarget = hiddenTarget || hidden
	}
	if !hiddenTarget {
		return nil
	}
	for _, c := range req.Channels { // as applying the layout would, before it finds the category missing
		if err := CheckSortOrder(c.SortOrder); err != nil {
			return err
		}
	}
	return errLayoutMissing
}

// errLayoutMissing is what applying a layout answers for a channel or a target
// category that does not exist.
var errLayoutMissing = httpx.ErrNotFound("channel or category not found")

// targetCategory returns the category a placement moves to, and whether the
// caller cannot see it. A hidden one counts as no category at all for the
// checks that follow, as a missing one does.
func targetCategory(snap *perm.Snapshot, sub perm.Subject, target *uuid.UUID) (to uuid.UUID, hidden bool) {
	if target == nil {
		return uuid.Nil, false
	}
	if snap.KnowsCategory(*target) && !snap.CategoryVisible(sub, *target) {
		return uuid.Nil, true
	}
	return *target, false
}

func categoryOverwritesAffected(snap *perm.Snapshot, from, to uuid.UUID) bool {
	if from == to {
		return false
	}
	return snap.HasCategoryOverwrites(from) || snap.HasCategoryOverwrites(to)
}

// checkChannelMove checks one placement of a layout change: the caller sees
// the channel and may move it between categories; hiddenTarget reports a target
// category the caller cannot see.
func checkChannelMove(snap *perm.Snapshot, sub perm.Subject, c ChannelPlacement) (hiddenTarget bool, err error) {
	if !snap.Channel(sub, c.ID).Has(perm.ViewChannel) {
		return false, perm.ErrChannelHidden
	}
	to, hiddenTarget := targetCategory(snap, sub, c.CategoryID)
	// Moving a channel takes on the permissions of its new category and
	// drops those of the old one, which could undo a restriction the
	// caller may not lift: with overwrites on either side, only an
	// Administrator (or the Owner) moves it.
	from, inCategory := snap.CategoryOf(c.ID)
	if !inCategory {
		from = uuid.Nil
	}
	if categoryOverwritesAffected(snap, from, to) && !snap.Global(sub).Has(perm.Administrator) {
		return false, httpx.ErrForbidden("moving a channel between categories with permissions needs the Administrator permission")
	}
	return hiddenTarget, nil
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
