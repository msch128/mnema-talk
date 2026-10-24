package discordimport

import (
	"fmt"
	"math/bits"

	"github.com/msch128/mnema-talk/internal/perm"
)

// discordBit is one Discord permission and what it becomes in Mnema Talk.
// To is zero for a permission Mnema Talk has no safe equivalent for: it is
// dropped and listed in the preview.
type discordBit struct {
	Name string
	To   perm.Set
}

// discordBits is the explicit, documented mapping (doc/import.md). The index
// is the bit number of Discord's permission field. Anything not listed here is
// unknown to us and dropped too.
//
// Deliberate choices: ADMINISTRATOR is never mapped (imported roles never hold
// Administrator); CREATE_INSTANT_INVITE is dropped because Mnema's invite
// permission opens the admin area; READ_MESSAGE_HISTORY is covered by view
// channels; MOVE_MEMBERS and the mute/deafen permissions have no counterpart.
var discordBits = map[int]discordBit{
	0:  {"CREATE_INSTANT_INVITE", 0},
	1:  {"KICK_MEMBERS", perm.KickMembers},
	2:  {"BAN_MEMBERS", perm.BanMembers},
	3:  {"ADMINISTRATOR", 0},
	4:  {"MANAGE_CHANNELS", perm.ManageChannels},
	5:  {"MANAGE_GUILD", 0},
	6:  {"ADD_REACTIONS", perm.AddReactions},
	7:  {"VIEW_AUDIT_LOG", perm.ViewAuditLog},
	8:  {"PRIORITY_SPEAKER", 0},
	9:  {"STREAM", perm.Video},
	10: {"VIEW_CHANNEL", perm.ViewChannel},
	11: {"SEND_MESSAGES", perm.SendMessages},
	12: {"SEND_TTS_MESSAGES", 0},
	13: {"MANAGE_MESSAGES", perm.ManageMessages},
	14: {"EMBED_LINKS", 0},
	15: {"ATTACH_FILES", perm.AttachFiles},
	16: {"READ_MESSAGE_HISTORY", 0},
	17: {"MENTION_EVERYONE", perm.MentionEveryone},
	18: {"USE_EXTERNAL_EMOJIS", 0},
	19: {"VIEW_GUILD_INSIGHTS", 0},
	20: {"CONNECT", perm.Connect},
	21: {"SPEAK", perm.Speak},
	22: {"MUTE_MEMBERS", 0},
	23: {"DEAFEN_MEMBERS", 0},
	24: {"MOVE_MEMBERS", 0},
	25: {"USE_VAD", 0},
	26: {"CHANGE_NICKNAME", 0},
	27: {"MANAGE_NICKNAMES", 0},
	28: {"MANAGE_ROLES", perm.ManageRoles},
	29: {"MANAGE_WEBHOOKS", 0},
	30: {"MANAGE_GUILD_EXPRESSIONS", perm.ManageEmoji},
	31: {"USE_APPLICATION_COMMANDS", 0},
	32: {"REQUEST_TO_SPEAK", 0},
	33: {"MANAGE_EVENTS", 0},
	34: {"MANAGE_THREADS", 0},
	35: {"CREATE_PUBLIC_THREADS", 0},
	36: {"CREATE_PRIVATE_THREADS", 0},
	37: {"USE_EXTERNAL_STICKERS", 0},
	38: {"SEND_MESSAGES_IN_THREADS", 0},
	39: {"USE_EMBEDDED_ACTIVITIES", 0},
	40: {"MODERATE_MEMBERS", perm.TimeoutMembers},
	41: {"VIEW_CREATOR_MONETIZATION_ANALYTICS", 0},
	42: {"USE_SOUNDBOARD", 0},
	43: {"CREATE_GUILD_EXPRESSIONS", 0},
	44: {"CREATE_EVENTS", 0},
	45: {"USE_EXTERNAL_SOUNDS", 0},
	46: {"SEND_VOICE_MESSAGES", 0},
	49: {"SEND_POLLS", 0},
	50: {"USE_EXTERNAL_APPS", 0},
}

// Mapped is the result of mapping one Discord permission field.
type Mapped struct {
	Set perm.Set
	// Dropped names the Discord permissions that became nothing, in bit order.
	Dropped []string
	// AdministratorDropped says the ADMINISTRATOR bit was set and dropped.
	AdministratorDropped bool
}

// mapBits maps a Discord permission field. Mapped permissions are limited to
// allowed: what the importing administrator may hand out, and for overwrites
// what a channel overwrite can carry. A bit that maps to something outside
// allowed is dropped and named like any other dropped bit.
func mapBits(field uint64, allowed perm.Set) Mapped {
	var m Mapped
	for field != 0 {
		n := bits.TrailingZeros64(field)
		field &^= 1 << n
		b, known := discordBits[n]
		switch {
		case !known:
			m.Dropped = append(m.Dropped, fmt.Sprintf("UNKNOWN_BIT_%d", n))
		case n == 3:
			m.AdministratorDropped = true
			m.Dropped = append(m.Dropped, b.Name)
		case b.To == 0 || !allowed.Has(b.To):
			m.Dropped = append(m.Dropped, b.Name)
		default:
			m.Set |= b.To
		}
	}
	return m
}

// keys lists the API keys of the permissions in s, in display order.
func keys(s perm.Set) []string {
	out := []string{}
	for _, info := range perm.Catalog {
		if s.Has(info.Bit) {
			out = append(out, info.Key)
		}
	}
	return out
}

// grantable is what an importing administrator may put on an imported role:
// what they hold themselves, without Administrator and without the powers only
// the Owner hands out.
func grantable(actor perm.Set) perm.Set {
	return actor &^ perm.Administrator &^ perm.OwnerGrantOnly
}
