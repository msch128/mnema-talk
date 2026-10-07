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
  server_info: [{ version: '0.4.4' }],
  system_update: [{ version: '0.5.0' }],
  presence_snapshot: [[], [id], {}, { [id]: 'online' }, { [id]: 'offline' }],
  presence_update: [{ user_id: id, status: 'focus' }],
  member_joined: [userFixture()],
  channels_changed: [null],
  typing: [room],
  read_state: [
    { channel_id: id, last_read_at: null, unread_count: 1, mention_count: 0 },
    { channel_id: id, last_read_at: FIXTURE_TIMESTAMP, unread_count: 0, mention_count: 0 },
    { channel_id: id, last_read_at: FIXTURE_TIMESTAMP, refresh: true },
    { channel_id: id, notify_level: 'all' },
    { channel_id: id, notify_level: 'mentions' },
    { channel_id: id, notify_level: 'mute' },
  ],
  message_create: [messageFixture()],
  message_update: [messageFixture({ is_edited: true })],
  message_delete: [{ id, channel_id: id, parent_id: null }, { id, channel_id: id, parent_id: id }],
  message_reaction: [{ message_id: id, reactions: [] }, { message_id: id, reactions: [reaction] }],
  user_update: [userFixture(), { id, disabled: false }, { id, disabled: true }],
  webrtc_offer: [{ type: 'offer', sdp: 'v=0\r\n' }],
  webrtc_candidate: [{ candidate: '' }, { candidate: 'candidate:synthetic', sdpMid: '0', sdpMLineIndex: 0, usernameFragment: 'synthetic' }],
  voice_snapshot: [{}, { [id]: {} }, { [id]: { [id]: voiceUser } }],
  voice_state_update: [{ action: 'leave', channel_id: id, user_id: id }, { action: 'join', channel_id: id, user: voiceUser, started_at: FIXTURE_TIMESTAMP }],
  voice_rooms: [{ started: {}, now: FIXTURE_TIMESTAMP }, { started: { [id]: FIXTURE_TIMESTAMP }, now: FIXTURE_TIMESTAMP }],
  user_stats: [{ user_id: id, voice_seconds: 123 }],
  webrtc_media_state: [{ ...room, screen: true, camera: false }],
  screen_viewers: [{ ...room, viewers: [] }, { ...room, viewers: [id] }],
  voice_speaking: [{ ...room, active: true }],
  voice_mute_state: [{ ...room, muted: true, deafened: false }],
  voice_kicked: [{ channel_id: id }, { channel_id: id, reason: 'room_full' }],
}

describe('WebSocket runtime event contracts', () => {
  it('preserves capacity rejection reasons while rejecting malformed or future reason values', () => {
    const event = decodeServerEvent({ type: 'voice_kicked', payload: { channel_id: id, reason: 'room_full' } })
    expect(event?.type).toBe('voice_kicked')
    if (event?.type !== 'voice_kicked') throw new Error('Expected voice rejection')
    expect(event.payload.reason).toBe('room_full')
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
    for (const value of [null, { ...voiceUser, joined_at: null }, { ...voiceUser, muted: 0 }, { ...voiceUser, deafened: null }, { ...voiceUser, id: 'invalid' }]) expect(isVoiceUser(value)).toBe(false)
  })
})
