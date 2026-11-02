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
	// ManageBots creates bot accounts and sets their tokens, scope,
	// capabilities and events (doc/bots.md). Only the Owner can grant it.
	ManageBots
	// ViewAuditLog reads the audit log (doc/moderation.md). Explicit value:
	// 1<<16 belongs to Manage bots.
	ViewAuditLog Set = 1 << 17
	// TimeoutMembers silences members below the actor's highest role for a
	// while (doc/moderation.md).
	TimeoutMembers Set = 1 << 18
	// BanMembers bans and unbans members below the actor's highest role and
	// lists the bans.
	BanMembers Set = 1 << 19
	// ManageEmoji uploads, renames and deletes the custom emoji of the
	// instance.
	ManageEmoji Set = 1 << 20
	// ManageReports reads and resolves member reports of messages, including
	// the context a reporter chose to share from a direct message
	// (doc/moderation.md). It is the only way a moderator sees any part of a
	// direct message conversation.
	ManageReports Set = 1 << 21
	// SendDirectMessages starts direct message conversations and writes in
	// them (doc/architecture.md, "Direct messages"). On for @everyone; an
	// administrator can withhold it from new members or onboarding roles.
	// Reading a conversation one is in never needs it.
	SendDirectMessages Set = 1 << 22
	// KickMembers signs members below the actor's highest role out of every
	// device and drops them from the Talk they are in, with no lasting
	// sanction: they can sign in again at once (doc/moderation.md). Bit 22 is
	// the direct-message permission.
	KickMembers Set = 1 << 23
	// ManageStage gives and takes talk power in a Stage and answers raised
	// hands (doc/talks.md). Holders can always speak there. Owner and
	// Administrators only by default; it can be allowed per Stage.
	ManageStage Set = 1 << 24
	// ManageEvents creates, edits and cancels community events.
	ManageEvents Set = 1 << 25
	// BypassNewMemberLimits exempts a member from the minimum membership age
	// before writing, reacting and speaking (doc/moderation.md, raid
	// protection). Administrators and the Owner never wait. Separate from the
	// slow mode exemption, which Manage messages gives.
	BypassNewMemberLimits Set = 1 << 26
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
	{Key: "manage_bots", Bit: ManageBots, Name: "manage bots"},
	{Key: "view_audit_log", Bit: ViewAuditLog, Name: "view the audit log"},
	{Key: "timeout_members", Bit: TimeoutMembers, Name: "timeout members"},
	{Key: "ban_members", Bit: BanMembers, Name: "ban members"},
	{Key: "manage_emoji", Bit: ManageEmoji, Name: "manage emoji"},
	{Key: "manage_reports", Bit: ManageReports, Name: "manage reports"},
	{Key: "send_direct_messages", Bit: SendDirectMessages, Name: "send direct messages"},
	{Key: "kick_members", Bit: KickMembers, Name: "kick members"},
	{Key: "manage_stage", Bit: ManageStage, Name: "manage Stages", Channel: true},
	{Key: "manage_events", Bit: ManageEvents, Name: "manage events"},
	{Key: "bypass_new_member_limits", Bit: BypassNewMemberLimits, Name: "bypass new member limits"},
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
	DefaultEveryone = ViewChannel | SendMessages | AttachFiles | AddReactions | MentionEveryone | Connect | Speak | Video | SendDirectMessages
	// OwnerGrantOnly can only be handed out by the Owner.
	OwnerGrantOnly = ManageBots
	// AdminArea is any permission that opens a part of the admin area.
	AdminArea = ManageChannels | ManageRoles | CreateInvites | ManageWebhooks | ManageMembers | DisconnectMembers | ViewAuditLog | TimeoutMembers | BanMembers | ManageBots | ManageEmoji | ManageReports | KickMembers
)

// TimeoutMask is what a timeout takes from a member: they can still read.
const TimeoutMask = SendMessages | AttachFiles | AddReactions | MentionEveryone | Connect | Speak | Video | SendDirectMessages

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
