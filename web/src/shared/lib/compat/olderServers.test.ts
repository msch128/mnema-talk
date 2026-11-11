import { describe, expect, it } from 'vitest'
import {
  decodeChatMessage, decodeChatNotificationSettings, decodeChatThread, decodeDiscordimportPlan, decodeOnboardingSettings,
  decodeServerBackupStatus, decodeServerCapacityEstimate, decodeServerLegal, decodeServerOpsSample, decodeServerVoiceHealth,
  isChatForumTag, isChatMessage
} from '../../../types/rest'
import { arrayDecoder } from '../../../types/validation'
import { decodeServerEvent } from '../../../types/events'
import { fillMissing } from './olderServers'

const ID = '00000000-0000-4000-8000-000000000001'
const AT = '2026-01-01T00:00:00Z'
const tag = (name: string, extra = {}) => ({ channel_id: ID, created_at: AT, id: ID, name, ...extra })
const message = (tags?: unknown[]) => ({
  attachments: [], channel_id: ID, content: 'hi', created_at: AT, display_name: 'A', id: ID, is_edited: false, is_pinned: false,
  mentions: [], number: 1, reactions: [], reply_count: 0, updated_at: AT, user_id: ID, username: 'a', ...(tags ? { tags } : {})
})

describe('fillMissing', () => {
  it('fills only missing keys and never changes the input', () => {
    const input = { a: 1 }
    expect(fillMissing(input, () => ({ a: 2, b: 3 }))).toEqual({ a: 1, b: 3 })
    expect(input).toEqual({ a: 1 })
  })

  it('returns the same object when nothing is missing and leaves non-records alone', () => {
    const input = { a: 1 }
    expect(fillMissing(input, () => ({ a: 2 }))).toBe(input)
    expect(fillMissing(null, () => ({ a: 2 }))).toBeNull()
  })
})

describe('forum tags of older servers', () => {
  it('get moderated=false and their place in the list, wherever messages carry them', () => {
    const decoded = decodeChatMessage(message([tag('a'), tag('b')]))
    expect(decoded.tags?.map(t => [t.moderated, t.position])).toEqual([[false, 1], [false, 2]])
    const thread = decodeChatThread({ root: message([tag('a')]), replies: [message([tag('b')])] })
    expect(thread.replies[0]?.tags?.[0]?.moderated).toBe(false)
    expect(thread.root.tags?.[0]?.position).toBe(1)
  })

  it('keep what the server sent', () => {
    const decoded = decodeChatMessage(message([tag('a', { moderated: true, position: 7 })]))
    expect(decoded.tags?.[0]).toMatchObject({ moderated: true, position: 7 })
  })

  it('are filled in a list of tags and in an array of messages', () => {
    const tags = arrayDecoder('tags', isChatForumTag)([tag('a'), tag('b')])
    expect(tags.map(t => t.position)).toEqual([1, 2])
    const list = arrayDecoder('messages', isChatMessage)([message([tag('a')]), message()])
    expect(list[0]?.tags?.[0]?.moderated).toBe(false)
  })

  it('are filled in a message event over the socket', () => {
    const event = decodeServerEvent({ type: 'message_create', payload: message([tag('a')]) })
    expect(JSON.stringify(event)).toContain('"moderated":false')
  })
})

describe('other answers of older servers', () => {
  it('notification settings without a server level follow the default', () => {
    expect(decodeChatNotificationSettings({ categories: [], channels: [] }).server).toEqual({ level: 'all', muted_until: null, suppress_everyone: false })
  })

  it('a Discord import plan without role mapping or hints', () => {
    const plan = decodeDiscordimportPlan({
      administrator_dropped: false, categories: [], channels: [], code: 'abc', counts: { categories: 0, channels: 0, roles: 0 },
      dropped_bits: [], everyone: { applied: false, dropped: [], permission_keys: [] }, hash: 'h',
      roles: [{ administrator_dropped: false, color: 0, dropped: [], hoist: false, mentionable: false, name: 'r', permission_keys: [], permissions: 0, source_id: '1', source_name: 'r' }],
      server_name: 's', skipped: [], source_name: 's', template_name: 't'
    })
    expect(plan).toMatchObject({ mapped_roles: [], mappable_roles: [], skipped_hints: [] })
    expect(plan.roles[0]?.suggested_role_id).toBeNull()
  })

  it('onboarding settings without the switches take the server defaults', () => {
    const settings = decodeOnboardingSettings({ community_name: 'c', rules_text: '', rules_version: 0, start_channel_id: null, welcome_text: '' })
    expect(settings).toMatchObject({ direct_messages_enabled: true, session_inactivity_days: 7 })
  })

  it('legal data without the checks', () => {
    const legal = decodeServerLegal({
      legal_version: '1', media_retention_days: 0, operator_country: '', operator_email: '', operator_name: '', project_notice: '',
      session_expiry_days: 7, stun_servers: [], turn_servers: [], update_check: true
    })
    expect(legal).toMatchObject({ address_detection: true, clock_check: true, builtin_turn: false })
  })

  it('a capacity estimate without scenarios and upload limit', () => {
    const estimate = decodeServerCapacityEstimate({ basis: 'table', more_members: 12, per_member_millicores: 3.5, plan_cores: 2, scale: 1, status: 'ok' })
    expect(estimate).toMatchObject({ scenarios: [], upload_limit_mbps: 0 })
  })

  it('ops samples without packet rates', () => {
    const sample = { cpu_millicores: 1, db_connections_acquired: 1, egress_bps: 1, errors_5xx: 0, goroutines: 1, heap_bytes: 1, ice_failures: 0, ingress_bps: 1, online_users: 0, peers: 0, requests: 1, rss_bytes: 1, screen_shares: 0, t: 1, ws_connections: 0 }
    expect(decodeServerOpsSample(sample)).toMatchObject({ egress_pps: 0, ingress_pps: 0 })
  })

  it('voice health without a built-in relay', () => {
    const voice = { cameras: 0, enabled: true, media_connections: 0, online_users: 0, participants: 0, rooms: 0, screen_shares: 0, stun_configured: false, turn_configured: false, websocket_connections: 0 }
    expect(decodeServerVoiceHealth(voice).builtin_turn).toMatchObject({ mode: 'off', active: false })
  })
})

describe('backup status of a server before the structured status', () => {
  const old = { last_backup_age: '7h29m', last_restore_test_age: '1h5m30s', hint: 'doc/operations.md#backups-and-restore' }

  it('reads the state from the two ages', () => {
    expect(decodeServerBackupStatus(old)).toMatchObject({
      state: 'ok', warn: false, stale: false, stale_after_hours: 36, last_backup_age_seconds: 26940,
      restore_test: 'none', restore_test_age_seconds: 3930, last_backup_age: '7h29m'
    })
  })

  it('is missing without a readable age, and stale after the limit', () => {
    expect(decodeServerBackupStatus({ ...old, last_backup_age: 'unknown', last_restore_test_age: 'unknown' })).toMatchObject({ state: 'missing', restore_test: 'none' })
    expect(decodeServerBackupStatus({ ...old, last_backup_age: '40h0m0s' })).toMatchObject({ stale: true, warn: true })
  })

  it('keeps a state the server sent', () => {
    expect(decodeServerBackupStatus({ ...old, state: 'not_configured' })).toMatchObject({ state: 'not_configured' })
  })
})
