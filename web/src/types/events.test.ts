import { describe, expect, it } from 'vitest'
import { decodeServerEvent, isIceCandidate, isPresence, isServerEvent, isVoiceUser } from './events'
import type { ServerEventType } from './events'
import { ContractError } from './validation'
import { fixtureId, FIXTURE_TIMESTAMP, messageFixture, userFixture } from '../test-fixtures.fixture'

const id = fixtureId(1)
const room = { channel_id: fixtureId(2), user_id: id }
const voiceUser = { ...userFixture(), joined_at: FIXTURE_TIMESTAMP, muted: false, deafened: true }
const reaction = { emoji: '👍', count: 1, users: [id] }
const examples: Record<ServerEventType, unknown[]> = {
  pong: [{ t: 0 }, { t: 0.5 }],
  server_info: [{ version: '0.4.4' }, { version: '0.8.0', screen_layers: 3 }, { version: '0.8.0', screen_layers: 0 }, { version: '2.2.0', screen_layers: 3, camera_layers: 3 }],
  system_update: [{ version: '0.5.0' }],
  presence_snapshot: [[], [id], {}, { [id]: 'online' }, { [id]: 'offline' }],
  presence_update: [{ user_id: id, status: 'focus' }, { user_id: id, status: 'dnd', activity: { type: 0, name: 'chess' } }],
  member_joined: [userFixture()],
  channels_changed: [null],
  permissions_changed: [null],
  roles_changed: [null],
  emoji_changed: [null],
  thread_follow_updated: [{ thread_id: id, channel_id: id, following: true }, { thread_id: id, channel_id: id, following: false }],
  saved_updated: [{ message_id: id, saved: true }, { message_id: id, saved: false }],
  reminder_update: [{ id, state: 'created' }, { id, state: 'due' }, { id, state: 'removed' }],
  scheduled_message_update: [{ id, channel_id: id, state: 'sent' }, { id, channel_id: id, state: 'failed', reason: 'slow_mode' }],
  dm_created: [{
    id, group: false, members: [{ id, username: 'bob', display_name: 'Bob' }], created_at: FIXTURE_TIMESTAMP, last_message_at: null,
    unread_count: 0, mention_count: 0, last_read_at: null, notify_level: 'all', can_send: true, hidden: false, favourite_position: null
  }],
  dm_member_added: [{ dm_id: id, member: { id, username: 'bob', display_name: 'Bob', avatar_url: '/api/media/x' } }],
  dm_member_removed: [{ dm_id: id, member: { id, username: 'bob', display_name: 'Bob' } }],
  dm_prefs_updated: [{ dm_id: id }, { dm_id: null }],
  dm_call_ring: [{ conversation_id: id, talk_id: fixtureId(2), message_id: fixtureId(3), caller_id: id, talk_name: 'Talk 1', ring_seconds: 45 }],
  dm_call_end: [
    { conversation_id: id, talk_id: fixtureId(2), reason: 'answered', active: true, user_id: id },
    { conversation_id: id, talk_id: fixtureId(2), reason: 'cancelled', active: false, message_id: fixtureId(3) },
    { conversation_id: id, talk_id: fixtureId(2), reason: 'timeout', active: true, message_id: fixtureId(3) },
    { conversation_id: id, talk_id: fixtureId(2), reason: 'ended', active: false }
  ],
  block_updated: [{ user_id: id, blocked: true }, { user_id: id, blocked: false }],
  typing: [room],
  read_state: [
    { channel_id: id, last_read_at: null, unread_count: 1, mention_count: 0 },
    { channel_id: id, last_read_at: FIXTURE_TIMESTAMP, unread_count: 0, mention_count: 0 },
    { channel_id: id, last_read_at: FIXTURE_TIMESTAMP, refresh: true },
    { channel_id: id, notify_level: 'all' },
    { channel_id: id, notify_level: 'mentions' },
    { channel_id: id, notify_level: 'mute' },
    { refresh: true },
  ],
  message_create: [messageFixture()],
  message_update: [messageFixture({ is_edited: true })],
  message_delete: [{ id, channel_id: id, parent_id: null }, { id, channel_id: id, parent_id: id }],
  message_reaction: [{ message_id: id, reactions: [] }, { message_id: id, reactions: [reaction] }, { message_id: id, channel_id: id, reactions: [reaction] }],
  poll_update: [
    { message_id: id, channel_id: id, poll: { multiple: false, anonymous: true, closed: false, closes_at: FIXTURE_TIMESTAMP, total_voters: 1, options: [{ id, label: 'Yes', votes: 1 }] } },
    { message_id: id, channel_id: id, poll: { multiple: true, anonymous: false, closed: true, closes_at: FIXTURE_TIMESTAMP, total_voters: 1, options: [{ id, label: 'Yes', votes: 1, voters: [id] }] } },
  ],
  pins_changed: [{ channel_id: id, message_id: id, pinned: true, actor_id: id, actor_name: 'Mod' }, { channel_id: id, message_id: id, pinned: false, actor_id: id, actor_name: '' }],
  interaction_response: [
    { channel_id: id, bot_id: id, bot_name: 'helper', content: 'psst', embeds: null, created_at: FIXTURE_TIMESTAMP },
    { channel_id: id, bot_id: id, bot_name: 'helper', bot_avatar_url: '/api/media/x', content: '', embeds: [{ title: 'T', description: 'D' }, {}], created_at: FIXTURE_TIMESTAMP }
  ],
  user_update: [userFixture(), { id, disabled: false }, { id, disabled: true }],
  member_timeout: [{ id, timeout_until: FIXTURE_TIMESTAMP }, { id, timeout_until: null }],
  moderation_notice: [
    { kind: 'timeout', until: FIXTURE_TIMESTAMP, duration_seconds: 3600, reason: 'spam' },
    { kind: 'timeout_removed', reason: '' },
    { kind: 'ban', reason: 'no' },
    { kind: 'message_deleted', reason: 'rude', channel_name: 'general' },
    { kind: 'messages_deleted', reason: '', channel_name: 'general', count: 3 },
  ],
  kicked: [{ reason: '' }, { reason: 'too loud' }],
  webrtc_offer: [{ type: 'offer', sdp: 'v=0\r\n' }],
  webrtc_candidate: [{ candidate: '' }, { candidate: 'candidate:synthetic', sdpMid: '0', sdpMLineIndex: 0, usernameFragment: 'synthetic' }],
  voice_snapshot: [{}, { [id]: {} }, { [id]: { [id]: voiceUser } }],
  voice_state_update: [{ action: 'leave', channel_id: id, user_id: id }, { action: 'join', channel_id: id, user: voiceUser, started_at: FIXTURE_TIMESTAMP }],
  voice_rooms: [{ started: {}, now: FIXTURE_TIMESTAMP }, { started: { [id]: FIXTURE_TIMESTAMP }, now: FIXTURE_TIMESTAMP }],
  user_stats: [{ user_id: id, voice_seconds: 123 }],
  webrtc_media_state: [{ ...room, screen: true, camera: false }],
  screen_viewers: [{ ...room, viewers: [] }, { ...room, viewers: [id] }],
  screen_layers: [{ ...room, layers: [] }, { ...room, layers: [{ width: 1920, height: 1080, fps: 30 }, { width: 1280, height: 720, fps: 30 }] }],
  screen_quality: [{ ...room, layer: -1, auto: true }, { ...room, layer: 2, auto: false }],
  screen_layers_wanted: [{ channel_id: id, layers: [] }, { channel_id: id, layers: [0, 2] }, { channel_id: id, layers: [2], source: 'camera' }],
  cameras_capped: [{ channel_id: id, user_ids: [] }, { channel_id: id, user_ids: [id, fixtureId(2)] }],
  voice_speaking: [{ ...room, active: true }],
  voice_mute_state: [{ ...room, muted: true, deafened: false }],
  voice_kicked: [{ channel_id: id }, { channel_id: id, reason: 'room_full' }, { channel_id: id, reason: 'missing_permission' }, { channel_id: id, reason: 'talk_rate_limited' }, { channel_id: id, reason: 'talk_unavailable' }, { channel_id: id, reason: 'connection_lost' }, { channel_id: id, reason: 'media_unavailable' }, { channel_id: id, reason: 'joined_elsewhere' }, { channel_id: id, reason: 'voice_taken_over' }],
  talk_leader: [{ channel_id: id, leader_id: id }, { channel_id: id, leader_id: null }],
  talk_moved: [{ from_channel_id: id, channel_id: fixtureId(2) }],
  stage_hand: [{ ...room, raised: true, at: FIXTURE_TIMESTAMP }, { ...room, raised: false }],
  stage_talk_power: [{ ...room, granted: true }, { ...room, granted: false }],
  talk_reaction: [{ ...room, emoji: '🎉' }, { ...room, emoji: `<:party:${fixtureId(3)}>` }],
  media_handshake: [{ channel_id: id, user_id: id, wire: 'a' }],
  forum_tags_changed: [{ channel_id: id }],
  event_update: [
    {
      id,
      title: 'Game Night',
      description: 'Playing games',
      start_time: FIXTURE_TIMESTAMP,
      status: 'scheduled',
      created_at: FIXTURE_TIMESTAMP,
      updated_at: FIXTURE_TIMESTAMP,
      rsvp_counts: { going: 1, interested: 0, not_going: 0 },
    },
    {
      id,
      title: 'Community Call',
      description: 'Monthly sync',
      start_time: FIXTURE_TIMESTAMP,
      end_time: FIXTURE_TIMESTAMP,
      channel_id: id,
      location: null,
      cover_url: '/api/media/x',
      status: 'active',
      created_by: id,
      created_at: FIXTURE_TIMESTAMP,
      updated_at: FIXTURE_TIMESTAMP,
      rsvp_counts: { going: 2, interested: 1, not_going: 0 },
      my_rsvp: 'going',
      going_members: [{ id, username: 'bob', display_name: 'Bob', avatar_url: null }],
    }
  ],
  event_delete: [{ id }],
  event_reminder: [
    { event_id: id, title: 'Game Night', start_time: FIXTURE_TIMESTAMP },
    { event_id: id, title: 'Talk Event', start_time: FIXTURE_TIMESTAMP, channel_id: id }
  ],
  community_updated: [{ community_name: 'Lantern Fox' }, { community_name: '' }],
}

describe('WebSocket runtime event contracts', () => {
  it('preserves capacity rejection reasons while rejecting malformed or future reason values', () => {
    const event = decodeServerEvent({ type: 'voice_kicked', payload: { channel_id: id, reason: 'room_full' } })
    expect(event?.type).toBe('voice_kicked')
    if (event?.type !== 'voice_kicked') throw new Error('Expected voice rejection')
    expect(event.payload.reason).toBe('room_full')
    const denied = decodeServerEvent({ type: 'voice_kicked', payload: { channel_id: id, reason: 'missing_permission' } })
    expect(denied?.type === 'voice_kicked' && denied.payload.reason).toBe('missing_permission')
    for (const reason of [undefined, null, '', 'admin', 'unknown']) {
      expect(() => decodeServerEvent({ type: 'voice_kicked', payload: { channel_id: id, reason } })).toThrow(ContractError)
    }
    expect(decodeServerEvent({ type: 'voice_kicked', payload: { channel_id: id } })).toEqual({ type: 'voice_kicked', payload: { channel_id: id } })
  })
  for (const [type, variants] of Object.entries(examples)) {
    it(`${type}: accepts actual Go variants without changing their contents`, () => {
      for (const payload of variants) {
        const event = { type, payload }
        expect(isServerEvent(event)).toBe(true)
        expect(decodeServerEvent(event)).toBe(event)
      }
    })
    it(`${type}: rejects malformed payload roots and individually corrupted fields`, () => {
      for (const payload of [undefined, 1, 'synthetic', false]) {
        expect(isServerEvent({ type, payload })).toBe(false)
        expect(() => decodeServerEvent({ type, payload })).toThrow(ContractError)
      }
      for (const valid of variants) {
        if (valid === null || typeof valid !== 'object' || Array.isArray(valid)) continue
        for (const key of Object.keys(valid)) {
          for (const corrupt of [undefined, { malformed: true }, false]) {
            if (typeof Reflect.get(valid, key) === 'boolean' && corrupt === false) continue
            if (type === 'webrtc_candidate' && key !== 'candidate' && corrupt === undefined) continue
            const payload = { ...valid, [key]: corrupt }
            expect(isServerEvent({ type, payload }), `${type}.${key}`).toBe(false)
          }
        }
      }
    })
  }

  it('rejects invalid envelopes and leaves future event names ignorable', () => {
    for (const value of [null, undefined, [], 1, {}, { type: null }, { type: 17 }, { payload: {} }]) {
      expect(isServerEvent(value)).toBe(false)
      expect(() => decodeServerEvent(value)).toThrow(ContractError)
    }
    expect(decodeServerEvent({ type: 'future', payload: { unknown: true } })).toBeNull()
    expect(isServerEvent({ type: 'future', payload: {} })).toBe(false)
    expect(isServerEvent({ type: 'pong', payload: { t: Infinity } })).toBe(false)
    expect(isServerEvent({ type: 'voice_state_update', payload: { action: 'wrong', channel_id: id } })).toBe(false)
  })

  it('does not allow valid notification/refresh tags to smuggle invalid state into store merges (R1-C1)', () => {
    const notify = { channel_id: id, notify_level: 'all' }
    const refresh = { channel_id: id, last_read_at: FIXTURE_TIMESTAMP, refresh: true }
    const malformed: unknown[] = [
      { ...notify, unread_count: 'invalid', last_read_at: { malformed: true } },
      { ...notify, refresh: false },
      { ...notify, mention_count: [] },
      { ...notify, last_read_at: undefined },
      { ...refresh, unread_count: 'invalid' },
      { ...refresh, mention_count: null },
      { ...refresh, notify_level: 'invalid' },
      { ...refresh, refresh: false },
      { ...refresh, last_read_at: null },
      { channel_id: id, notify_level: 'invalid' },
      { refresh: false },
      { refresh: true, unread_count: 1 },
      {},
    ]
    for (const payload of malformed) expect(() => decodeServerEvent({ type: 'read_state', payload })).toThrow(ContractError)
    for (const payload of examples.read_state) expect(decodeServerEvent({ type: 'read_state', payload })).toEqual({ type: 'read_state', payload })
  })

  it('does not fall back to a disabled-only update after malformed profile validation (R1-C2)', () => {
    for (const fields of [
      { display_name: { malformed: true } }, { username: 4 }, { role: 'admin' },
      { avatar_url: null }, { bio: [] }, { created_at: 'invalid' }, { locale: false },
      { message_count: 1.5 }, { presence: 'invalid' }, { status_text: {} }, { voice_seconds: null },
    ]) {
      for (const disabled of [false, true]) {
        expect(() => decodeServerEvent({ type: 'user_update', payload: { id, disabled, ...fields } })).toThrow(ContractError)
      }
    }
    expect(() => decodeServerEvent({ type: 'user_update', payload: { ...userFixture(), disabled: 'invalid' } })).toThrow(ContractError)
    for (const payload of examples.user_update) expect(decodeServerEvent({ type: 'user_update', payload })).toEqual({ type: 'user_update', payload })
  })
})

describe('ICE, voice-user and presence leaf contracts', () => {
  it('validates every optional ICE field independently, including null and boundaries', () => {
    for (const candidate of [
      { candidate: '' },
      { candidate: '', sdpMid: null, sdpMLineIndex: null, usernameFragment: null },
      { candidate: '', sdpMid: undefined, sdpMLineIndex: undefined, usernameFragment: undefined },
      { candidate: 'x'.repeat(16384), sdpMid: 'x'.repeat(256), sdpMLineIndex: 65535, usernameFragment: 'x'.repeat(256) },
    ]) expect(isIceCandidate(candidate)).toBe(true)
    for (const candidate of [null, [], {}, { candidate: 'x'.repeat(16385) },
      { candidate: '', sdpMid: 1 }, { candidate: '', sdpMid: 'x'.repeat(257) },
      { candidate: '', sdpMLineIndex: -1 }, { candidate: '', sdpMLineIndex: 65536 }, { candidate: '', sdpMLineIndex: 0.5 },
      { candidate: '', usernameFragment: 1 }, { candidate: '', usernameFragment: 'x'.repeat(257) },
    ]) expect(isIceCandidate(candidate)).toBe(false)
  })
  it('accepts all supported presences and rejects unexpected ones', () => {
    for (const status of ['online', 'away', 'dnd', 'focus', 'offline']) expect(isPresence(status)).toBe(true)
    for (const status of [null, '', 'idle', 1, {}]) expect(isPresence(status)).toBe(false)
  })
  it('requires a complete user plus joined-at and both boolean mute flags', () => {
    expect(isVoiceUser(voiceUser)).toBe(true)
    expect(isVoiceUser({ ...voiceUser, talk_power: true, hand_raised_at: FIXTURE_TIMESTAMP })).toBe(true)
    for (const value of [{ ...voiceUser, talk_power: 1 }, { ...voiceUser, hand_raised_at: 'soon' }]) expect(isVoiceUser(value)).toBe(false)
    for (const value of [null, { ...voiceUser, joined_at: null }, { ...voiceUser, muted: 0 }, { ...voiceUser, deafened: null }, { ...voiceUser, id: 'invalid' }]) expect(isVoiceUser(value)).toBe(false)
  })
})

describe('screen layer events', () => {
  const layer = { width: 1280, height: 720, fps: 30 }
  const valid = (type: string, payload: unknown) => isServerEvent({ type, payload })

  it('limits layers to what the server takes', () => {
    expect(valid('screen_layers', { ...room, layers: [layer, layer, layer] })).toBe(true)
    expect(valid('screen_layers', { ...room, layers: [layer, layer, layer, layer] })).toBe(false)
    for (const bad of [{ ...layer, width: 0 }, { ...layer, height: -1 }, { ...layer, fps: 1.5 }, { ...layer, extra: 1 }, { width: 1, height: 1 }, null, 'x']) {
      expect(valid('screen_layers', { ...room, layers: [bad] })).toBe(false)
    }
    expect(valid('screen_layers', { ...room, layers: 'none' })).toBe(false)
  })

  it('accepts only a layer the share can have', () => {
    for (const layerIndex of [-1, 0, 1, 2]) expect(valid('screen_quality', { ...room, layer: layerIndex, auto: false })).toBe(true)
    for (const layerIndex of [-2, 3, 0.5, '1', null]) expect(valid('screen_quality', { ...room, layer: layerIndex, auto: false })).toBe(false)
    for (const index of [-1, 3, 1.5, '0']) expect(valid('screen_layers_wanted', { channel_id: id, layers: [index] })).toBe(false)
    expect(valid('screen_layers_wanted', { layers: [0] })).toBe(false)
  })

  it('takes the cameras the server caps as a list of users in a room', () => {
    expect(valid('cameras_capped', { channel_id: id, user_ids: ['not an id'] })).toBe(false)
    expect(valid('cameras_capped', { channel_id: id, user_ids: 'none' })).toBe(false)
    expect(valid('cameras_capped', { user_ids: [] })).toBe(false)
  })

  it('takes the camera layer count of a server_info and the source of a wanted announcement within bounds', () => {
    for (const count of [0, 1, 3]) expect(valid('server_info', { version: '1', camera_layers: count })).toBe(true)
    for (const count of [-1, 4, 1.5, '3', null]) expect(valid('server_info', { version: '1', camera_layers: count })).toBe(false)
    expect(valid('screen_layers_wanted', { channel_id: id, layers: [1], source: 'camera' })).toBe(true)
    for (const source of ['screen', 'audio', 1, null]) expect(valid('screen_layers_wanted', { channel_id: id, layers: [1], source })).toBe(false)
  })

  it('takes the layer count of a server_info within bounds', () => {
    for (const count of [0, 1, 3]) expect(valid('server_info', { version: '1', screen_layers: count })).toBe(true)
    for (const count of [-1, 4, 1.5, '3', null]) expect(valid('server_info', { version: '1', screen_layers: count })).toBe(false)
  })
})
