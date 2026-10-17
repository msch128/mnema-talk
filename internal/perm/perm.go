// Package perm is Mnema Talk's role and permission model.
//
// The Owner (the one account with users.role = 'admin', see ADMIN_USERNAME)
// holds every permission without exception and cannot be restricted by
// anyone. Everybody else gets the permissions of the implicit @everyone role
// plus those of the custom roles they were given; per-category and
// per-channel overwrites then refine them, Discord-style (see Snapshot).
//
// Permissions are bits of a Set. Their values are stored in the database and
// sent over the API, so existing bits must never change meaning.
package perm

import "math/bits"

// Set is a set of permissions, one bit each.
type Set uint64

// The permission bits. Append new ones at the end; never reorder or reuse.
const (
	// Administrator grants every permission below (the Owner-only powers stay
	// with the Owner) and ignores channel overwrites. Hierarchy still applies.
	Administrator Set = 1 << iota
	// ViewChannel lets a member see a channel (or Talk) and read its history.
	ViewChannel
	SendMessages
	AttachFiles
	AddReactions
	// ManageMessages deletes other members' messages.
	ManageMessages
	// MentionEveryone makes @all and @here notify people.
	MentionEveryone
	// Connect joins a Talk (voice channel).
	Connect
	// Speak sends audio in a Talk.
	Speak
	// Video sends camera and screen share in a Talk.
	Video
	// DisconnectMembers removes members from a Talk.
	DisconnectMembers
	// ManageChannels creates, edits, orders and deletes channels and
	// categories and edits their permission overwrites.
	ManageChannels
	// ManageRoles creates, edits, orders, deletes and assigns roles below the
	// member's own highest role.
	ManageRoles
	// CreateInvites creates, lists and revokes invite codes.
	CreateInvites
	// ManageWebhooks is reserved and unused: webhooks are an admin tool that
	// the Owner and Administrators manage (doc/webhooks.md), decoupled from
	// the other permissions. The bit keeps its place and meaning.
	ManageWebhooks
	// ManageMembers disables and re-enables accounts, ends their sessions and
	// edits their status line (members below the actor's highest role only).
	ManageMembers
	// ViewAuditLog reads the audit log (doc/moderation.md). Explicit value:
	// 1<<16 belongs to Manage bots.
	ViewAuditLog Set = 1 << 17
)

// Info describes one permission for clients.
type Info struct {
	Key  string `json:"key"`
	Bit  Set    `json:"bit" swaggertype:"integer" minimum:"1"`
	Name string `json:"-"`
	// Channel permissions can be overwritten per category and channel; the
	// rest only apply server-wide.
	Channel bool `json:"channel"`
}

// Catalog lists every permission in display order.
var Catalog = []Info{
	{Key: "administrator", Bit: Administrator, Name: "administrator"},
	{Key: "view_channel", Bit: ViewChannel, Name: "view channels", Channel: true},
	{Key: "send_messages", Bit: SendMessages, Name: "send messages", Channel: true},
	{Key: "attach_files", Bit: AttachFiles, Name: "attach files", Channel: true},
	{Key: "add_reactions", Bit: AddReactions, Name: "add reactions", Channel: true},
	{Key: "manage_messages", Bit: ManageMessages, Name: "manage messages", Channel: true},
	{Key: "mention_everyone", Bit: MentionEveryone, Name: "mention everyone", Channel: true},
	{Key: "connect", Bit: Connect, Name: "connect to Talks", Channel: true},
	{Key: "speak", Bit: Speak, Name: "speak", Channel: true},
	{Key: "video", Bit: Video, Name: "share video and screen", Channel: true},
	{Key: "disconnect_members", Bit: DisconnectMembers, Name: "disconnect members", Channel: true},
	{Key: "manage_channels", Bit: ManageChannels, Name: "manage channels"},
	{Key: "manage_roles", Bit: ManageRoles, Name: "manage roles"},
	{Key: "create_invites", Bit: CreateInvites, Name: "manage invites"},
	{Key: "manage_webhooks", Bit: ManageWebhooks, Name: "manage webhooks"},
	{Key: "manage_members", Bit: ManageMembers, Name: "manage members"},
	{Key: "view_audit_log", Bit: ViewAuditLog, Name: "view the audit log"},
}

var (
	// All is every defined permission.
	All = func() Set {
		var s Set
		for _, i := range Catalog {
			s |= i.Bit
		}
		return s
	}()
	// ChannelScoped are the permissions a category or channel overwrite may
	// allow or deny.
	ChannelScoped = func() Set {
		var s Set
		for _, i := range Catalog {
			if i.Channel {
				s |= i.Bit
			}
		}
		return s
	}()
)

const (
	// DefaultEveryone is what @everyone holds on a fresh install and what
	// migration 0017 seeds: exactly what every member could do before roles
	// existed. Do not widen it: upgrades must not change behavior.
	DefaultEveryone = ViewChannel | SendMessages | AttachFiles | AddReactions | MentionEveryone | Connect | Speak | Video
	// AdminArea is any permission that opens a part of the admin area.
	AdminArea = ManageChannels | ManageRoles | CreateInvites | ManageWebhooks | ManageMembers | DisconnectMembers | ViewAuditLog
)

// Has reports whether every bit of need is in s.
func (s Set) Has(need Set) bool { return s&need == need }

// HasAny reports whether at least one bit of mask is in s.
func (s Set) HasAny(mask Set) bool { return s&mask != 0 }

// Valid reports whether s only contains defined permissions.
func (s Set) Valid() bool { return s&^All == 0 }

// MissingName names the first permission of need that s lacks, in words for
// an error message; it is empty when s has them all.
func (s Set) MissingName(need Set) string {
	miss := need &^ s
	if miss == 0 {
		return ""
	}
	low := Set(1) << bits.TrailingZeros64(uint64(miss))
	for _, i := range Catalog {
		if i.Bit == low {
			return i.Name
		}
	}
	return "permission"
}
