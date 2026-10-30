// The ready-made choices of the quick menus (slow mode, invites, timeouts, polls,
// automod, mutes). They live in presets.json so the server tests can push every
// one of them through the real validation (internal/presets); a choice the
// server would refuse can then never ship.
import presets from './presets.json'

export const SLOW_MODE_PRESETS = presets.slowModeSeconds
export const INVITE_CREATE_HOURS = presets.inviteCreateHours
export const INVITE_EXPIRY_MINUTES = presets.inviteExpiryMinutes
export const INVITE_USES = presets.inviteUses
export const MODERATION_SECONDS = presets.moderationSeconds
export const POLL_DURATION_NAMES = presets.pollDurations
export const MUTE_MINUTES = presets.muteMinutes
export const ACCOUNT_AGE_SECONDS = presets.automodAccountAges
export const RAID_WINDOW_SECONDS = presets.automodRaidWindows
export const RAID_RESTRICTION_SECONDS = presets.automodRaidRestrictions
export const RULE_TIMEOUT_SECONDS = presets.automodRuleTimeouts
