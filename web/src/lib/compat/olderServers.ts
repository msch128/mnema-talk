// What this web app fills in when an older server leaves out a field that the current API contract requires.
//
// Servers served: 1.x, 2.0 to 2.8, and 2.9.12 to 2.9.18 (the releases web/src/compat holds the app against).
// The API contract itself (api/openapi.json, the Bot API included) stays as published: these fields are required
// there, and a current server always sends them. An older server did not know them yet, so each normaliser below
// names the schema it serves and puts in the documented default BEFORE the generated decoder checks the answer.
// A normaliser only fills keys that are missing; a key the server sent is never touched, and the input is never
// changed (a filled copy is returned).
//
// Registration is per guard (types/validation.ts): decoders, array decoders and the WebSocket message events
// all go through the same hook, so call sites do not change. Importing this module registers everything.
import {
  isChatForumTag, isChatMentionsPage, isChatMessage, isChatNotificationSettings, isChatSavedMessage, isChatSavedPage,
  isChatSearchResult, isChatStartTalkResponse, isChatThread, isDiscordimportPlan, isDiscordimportPlanChannel, isDiscordimportPlanRole,
  isOnboardingSettings, isServerBackupStatus,
  isServerCapacityEstimate, isServerCapacityStatus, isServerLegal, isServerOperatorStatus, isServerOpsSample, isServerOpsStatus,
  isServerSystemHealth, isServerSystemStatus, isServerVoiceHealth
} from '../../types/rest'
import { isRecord, registerNormaliser } from '../../types/validation'

type Rec = Record<string, unknown>

/** The record with the missing keys added from `defaults`; the same record when nothing is missing. */
export function fillMissing(value: unknown, defaults: () => Rec): unknown {
  if (!isRecord(value)) return value
  const wanted = defaults()
  const missing = Object.keys(wanted).filter(key => !Object.prototype.hasOwnProperty.call(value, key))
  if (missing.length === 0) return value
  return { ...Object.fromEntries(missing.map(key => [key, wanted[key]])), ...value }
}

/** `value[key]` run through `map`; the same record when the key is absent or nothing changed. */
function onKey(value: unknown, key: string, map: (inner: unknown) => unknown): unknown {
  if (!isRecord(value) || !Object.prototype.hasOwnProperty.call(value, key)) return value
  const inner = map(value[key])
  return inner === value[key] ? value : { ...value, [key]: inner }
}

function eachItem(map: (item: unknown, index: number) => unknown): (list: unknown) => unknown {
  return list => {
    if (!Array.isArray(list)) return list
    const mapped = list.map(map)
    return mapped.some((item, i) => item !== list[i]) ? mapped : list
  }
}

// chat.ForumTag (1.x to 2.8 sent no moderated flag and no position): not moderated, in list order starting at 1.
const forumTag = (tag: unknown, index: number): unknown => fillMissing(tag, () => ({ moderated: false, position: index + 1 }))
const forumTags = eachItem(forumTag)

// chat.Message carries its forum tags; every response and event that holds messages goes through this.
const message = (value: unknown): unknown => onKey(value, 'tags', forumTags)
const messages = eachItem(message)

// notify settings: a server before 1.1 sends no server-wide level, which then follows the default.
const notificationSettings = (value: unknown): unknown =>
  fillMissing(value, () => ({ server: { level: 'all', muted_until: null, suppress_everyone: false } }))

// discordimport.Plan: no role mapping (suggested_role_id null, no mapped or mappable roles) and no hints.
const planRole = (role: unknown): unknown => fillMissing(role, () => ({ suggested_role_id: null }))
const planChannel = (channel: unknown): unknown => fillMissing(channel, () => ({ note_hints: [] }))
const discordPlan = (value: unknown): unknown => {
  const filled = fillMissing(value, () => ({ mapped_roles: [], mappable_roles: [], skipped_hints: [] }))
  return onKey(onKey(filled, 'roles', eachItem(planRole)), 'channels', eachItem(planChannel))
}

// onboarding.Settings: direct messages on and a 7-day inactivity sign-out are the server defaults.
const onboardingSettings = (value: unknown): unknown =>
  fillMissing(value, () => ({ direct_messages_enabled: true, session_inactivity_days: 7 }))

// server.Legal: the checks are on by default (WEBRTC_AUTO_ANNOUNCE, CLOCK_CHECK_ENABLED); there was no built-in relay.
const legal = (value: unknown): unknown => fillMissing(value, () => ({ address_detection: true, clock_check: true, builtin_turn: false }))

// server.OpsSample: no packet rates before they were measured.
const opsSample = (value: unknown): unknown => fillMissing(value, () => ({ egress_pps: 0, ingress_pps: 0 }))
const opsStatus = (value: unknown): unknown => onKey(value, 'history', eachItem(opsSample))

// server.VoiceHealth: no built-in relay, so it is off and not running.
const voiceHealth = (value: unknown): unknown => fillMissing(value, () => ({
  builtin_turn: {
    mode: 'off', enabled: false, active: false, port: 0, udp: false, tcp: false, allocations: 0, host: '', reachability: 'unknown'
  }
}))
const systemHealth = (value: unknown): unknown => onKey(value, 'voice', voiceHealth)
const systemStatus = (value: unknown): unknown => onKey(value, 'health', systemHealth)

const GO_DURATION = /^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+(?:\.\d+)?)s)?$/
const STALE_AFTER_HOURS = 36

/** Seconds of a Go duration text ("7h29m", "45s"); undefined for anything else ("unknown", "never"). */
function secondsOf(text: unknown): number | undefined {
  const parts = typeof text === 'string' && text !== '' ? GO_DURATION.exec(text) : null
  if (!parts) return undefined
  return Math.round(Number(parts[1] ?? 0) * 3600 + Number(parts[2] ?? 0) * 60 + Number(parts[3] ?? 0))
}

/** The backup state of a server before the structured status: only the two ages as text were sent, so the state is read from them. */
function backupFacts(status: Rec): Rec {
  const backupAge = secondsOf(status['last_backup_age'])
  const restoreAge = secondsOf(status['last_restore_test_age'])
  const stale = backupAge !== undefined && backupAge > STALE_AFTER_HOURS * 3600
  return {
    state: backupAge === undefined ? 'missing' : 'ok',
    warn: stale,
    stale,
    stale_after_hours: STALE_AFTER_HOURS,
    restore_test: restoreAge === undefined ? 'none' : 'passed',
    ...(backupAge === undefined ? {} : { last_backup_age_seconds: backupAge }),
    ...(restoreAge === undefined ? {} : { restore_test_age_seconds: restoreAge })
  }
}

const backupStatus = (value: unknown): unknown => isRecord(value) ? fillMissing(value, () => backupFacts(value)) : value
const operatorStatus = (value: unknown): unknown => onKey(value, 'backup', backupStatus)

// server.CapacityEstimate: no scenarios; 0 is "no upload limit set".
const capacityEstimate = (value: unknown): unknown => fillMissing(value, () => ({ scenarios: [], upload_limit_mbps: 0 }))
const capacityStatus = (value: unknown): unknown => onKey(value, 'estimate', capacityEstimate)

// Containers of messages.
const thread = (value: unknown): unknown => onKey(onKey(value, 'root', message), 'replies', messages)
const messagePage = (value: unknown): unknown => onKey(value, 'messages', messages)
const savedMessage = (value: unknown): unknown => onKey(value, 'message', message)
const savedPage = (value: unknown): unknown => onKey(value, 'items', eachItem(savedMessage))
const startTalk = (value: unknown): unknown => onKey(value, 'message', message)

registerNormaliser(isChatForumTag, tag => forumTag(tag, 0), forumTags)
registerNormaliser(isChatMessage, message)
registerNormaliser(isChatThread, thread)
registerNormaliser(isChatSearchResult, messagePage)
registerNormaliser(isChatMentionsPage, messagePage)
registerNormaliser(isChatSavedMessage, savedMessage)
registerNormaliser(isChatSavedPage, savedPage)
registerNormaliser(isChatStartTalkResponse, startTalk)
registerNormaliser(isChatNotificationSettings, notificationSettings)
registerNormaliser(isDiscordimportPlan, discordPlan)
registerNormaliser(isDiscordimportPlanRole, planRole)
registerNormaliser(isDiscordimportPlanChannel, planChannel)
registerNormaliser(isOnboardingSettings, onboardingSettings)
registerNormaliser(isServerLegal, legal)
registerNormaliser(isServerOpsSample, opsSample)
registerNormaliser(isServerOpsStatus, opsStatus)
registerNormaliser(isServerVoiceHealth, voiceHealth)
registerNormaliser(isServerSystemHealth, systemHealth)
registerNormaliser(isServerSystemStatus, systemStatus)
registerNormaliser(isServerBackupStatus, backupStatus)
registerNormaliser(isServerOperatorStatus, operatorStatus)
registerNormaliser(isServerCapacityEstimate, capacityEstimate)
registerNormaliser(isServerCapacityStatus, capacityStatus)
