package perm

import (
	"time"

	"github.com/google/uuid"
)

// AllowDeny is one allow/deny pair of a channel or category.
type AllowDeny struct{ Allow, Deny Set }

// Scope is the overwrites of one category or channel.
type Scope struct {
	Roles   map[uuid.UUID]AllowDeny
	Members map[uuid.UUID]AllowDeny
}

// State is plain permission data, the input of NewSnapshot. The database
// loader fills a Snapshot from rows; this is the same state without them.
type State struct {
	EveryoneID     uuid.UUID
	Everyone       Set
	Roles          map[uuid.UUID]Role
	UserRoles      map[uuid.UUID][]uuid.UUID
	Owners, Bots   map[uuid.UUID]bool
	ChannelCat     map[uuid.UUID]uuid.UUID // uuid.Nil: uncategorized
	Stages         map[uuid.UUID]bool
	Categories     map[uuid.UUID]bool
	DMs            map[uuid.UUID]map[uuid.UUID]bool
	DMsDisabled    bool                    // the community's direct message switch is off
	DMTalks        map[uuid.UUID]uuid.UUID // Talk -> the conversation it belongs to
	ChannelOv      map[uuid.UUID]Scope
	CategoryOv     map[uuid.UUID]Scope
	GlobalUploadMB *int
	Timeouts       map[uuid.UUID]Timeout
	RulesVersion   int
	RulesAccepted  map[uuid.UUID]int
	// NewMember are the minimum membership ages; Joined when members joined.
	NewMember NewMemberRules
	Joined    map[uuid.UUID]time.Time
}

// NewSnapshot builds the Snapshot of st as loaded at the given time. The
// maps are shared, not copied.
func NewSnapshot(st State, loaded time.Time) *Snapshot {
	s := newSnapshot(loaded)
	s.everyoneID, s.everyone = st.EveryoneID, st.Everyone
	s.roles, s.userRoles, s.owners, s.bots = st.Roles, st.UserRoles, st.Owners, st.Bots
	s.channelCat, s.stages, s.categories, s.dms = st.ChannelCat, st.Stages, st.Categories, st.DMs
	s.dmEnabled = !st.DMsDisabled
	if st.DMTalks != nil {
		s.dmTalks = st.DMTalks
	}
	s.channelOv, s.categoryOv = scopes(st.ChannelOv), scopes(st.CategoryOv)
	s.globalUploadMB, s.timeouts = st.GlobalUploadMB, st.Timeouts
	s.rulesVersion, s.rulesAccepted = st.RulesVersion, st.RulesAccepted
	s.newMember = st.NewMember
	if st.Joined != nil {
		s.joined = st.Joined
	}
	return s
}

func scopes(in map[uuid.UUID]Scope) map[uuid.UUID]*scope {
	out := make(map[uuid.UUID]*scope, len(in))
	for k, v := range in {
		sc := &scope{roles: map[uuid.UUID]overwrite{}, members: map[uuid.UUID]overwrite{}}
		for r, o := range v.Roles {
			sc.roles[r] = overwrite{allow: o.Allow, deny: o.Deny}
		}
		for m, o := range v.Members {
			sc.members[m] = overwrite{allow: o.Allow, deny: o.Deny}
		}
		out[k] = sc
	}
	return out
}
