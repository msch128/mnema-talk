package chat

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
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
	var apiErr *httpx.APIError
	if errors.As(err, &apiErr) {
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
	return SendContextFor(h.Perms, r.Context(), ch, need)
}

// SendContextFor is SendContext for work that runs without a request (a
// scheduled message): ctx carries the author (auth.WithUser) and the
// permission state is read now, so a later change of rights applies.
func SendContextFor(perms *perm.Service, ctx context.Context, ch uuid.UUID, need perm.Set) (context.Context, error) {
	if perms == nil {
		return ctx, nil
	}
	snap, err := perms.ForChannel(ctx, ch)
	if err != nil {
		return nil, err
	}
	sub := auth.UserFrom(ctx).Subject()
	p := snap.Channel(sub, ch)
	if err := snap.Deny(sub, p, need); err != nil {
		return nil, err
	}
	ctx = WithSlowModeExempt(ctx, p.Has(perm.ManageMessages))
	return WithMentionPolicy(ctx, MentionPolicy{
		NoEveryone: !p.Has(perm.MentionEveryone),
		CanView:    func(id uuid.UUID) bool { return snap.Channel(perm.Subject{ID: id}, ch).Has(perm.ViewChannel) },
		Roles:      MentionableRoles(snap, p.Has(perm.MentionEveryone)),
	}), nil
}

// refusalFor turns a "missing permission" refusal into RULES_NOT_ACCEPTED when
// the caller's rules acceptance is what took the permission away.
//
// need is what the refused action required; a new member's wait for it is
// named when it is what took the permission away.
func (h *Handler) refusalFor(r *http.Request, err error, need perm.Set) error {
	var apiErr *httpx.APIError
	if h.Perms == nil || !errors.As(err, &apiErr) || apiErr.Code != httpx.CodeForbidden {
		return err
	}
	snap, serr := h.Perms.Snapshot(r.Context())
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

// MentionableRoles are the roles an author may mention: those marked
// mentionable, or every custom role for an author who may mention everyone
// (the Owner and Administrators always may). @everyone and the roles the
// server manages for bots cannot be mentioned by name.
func MentionableRoles(snap *perm.Snapshot, mayAll bool) []MentionRole {
	var out []MentionRole
	for _, r := range snap.Roles() {
		if r.Everyone || r.Managed || !(r.Mentionable || mayAll) {
			continue
		}
		id := r.ID
		out = append(out, MentionRole{ID: id, Name: r.Name, Members: func() []uuid.UUID { return snap.RoleMembers(id) }})
	}
	return out
}

// SendContext is Handler.sendContext for a message that another feature creates for
// the caller (a built-in slash command, a poll): the caller needs need in ch.
func SendContext(perms *perm.Service, r *http.Request, ch uuid.UUID, need perm.Set) (context.Context, error) {
	return SendContextFor(perms, r.Context(), ch, need)
}

// UploadContext is the send context of an upload (media package): the caller
// needs to view the channel, send messages and attach files.
func UploadContext(perms *perm.Service, r *http.Request, ch uuid.UUID) (context.Context, error) {
	return SendContextFor(perms, r.Context(), ch, perm.SendMessages|perm.AttachFiles)
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

// hiddenMessageCounts counts messages per author over the channels the caller
// may not view; nil when there are none (the stored totals apply). Only the
// hidden channels are scanned: the stored total minus these is what the caller
// may see.
func (h *Handler) hiddenMessageCounts(r *http.Request) (map[uuid.UUID]int64, error) {
	if h.Perms == nil {
		return nil, nil
	}
	snap, err := h.Perms.Snapshot(r.Context())
	if err != nil {
		return nil, err
	}
	hidden := snap.HiddenChannels(auth.UserFrom(r.Context()).Subject())
	if len(hidden) == 0 {
		return nil, nil
	}
	rows, err := h.DB.Query(r.Context(), `SELECT user_id, COUNT(*) FROM messages WHERE channel_id = ANY($1::uuid[]) GROUP BY user_id`, hidden)
	if err != nil {
		return nil, fmt.Errorf("count hidden messages: %w", err)
	}
	type count struct {
		User uuid.UUID
		N    int64
	}
	found, err := pgx.CollectRows(rows, pgx.RowToStructByPos[count])
	if err != nil {
		return nil, fmt.Errorf("count hidden messages: %w", err)
	}
	out := make(map[uuid.UUID]int64, len(found))
	for _, c := range found {
		out[c.User] = c.N
	}
	return out, nil
}

// visibleHierarchy drops the channels the caller may not view, and the
// categories that are hidden from them and have nothing left in them.
func (h *Handler) visibleHierarchy(r *http.Request, cats []Category, uncat []Channel) ([]Category, []Channel, error) {
	if h.Perms == nil {
		return cats, uncat, nil
	}
	ctx := r.Context()
	snap, err := h.Perms.Snapshot(ctx)
	if err != nil {
		return nil, nil, err
	}
	v := &hierarchyView{perms: h.Perms, ctx: ctx, sub: auth.UserFrom(ctx).Subject(), snap: snap}
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

// visibleReadStates keeps the read states of viewable channels.
func (h *Handler) visibleReadStates(r *http.Request, states []ReadState) ([]ReadState, error) {
	visible, err := h.viewableChannels(r)
	if err != nil {
		return states, err
	}
	if visible == nil {
		return states, nil
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

// checkWholeCategory is the check for deleting a category on a state read
// under the permission lock: the caller must see the category and every
// channel in it, and when it carries overwrites (deleting it copies them into
// its channels) must be an Administrator or the Owner.
func checkWholeCategory(snap *perm.Snapshot, sub perm.Subject, id uuid.UUID) error {
	if !snap.KnowsCategory(id) {
		return nil // the deletion itself reports a missing category
	}
	if !snap.CategoryVisible(sub, id) || !snap.CanSeeAllChannelsIn(sub, id) {
		return errCategoryNotFound
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
			return errCategoryNotFound
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
		return uuid.Nil, errCategoryNotFound
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
