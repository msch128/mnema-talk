import { describe, expect, it } from 'vitest'
import { channelFixture, fixtureId, FIXTURE_TIMESTAMP, messageFixture, userFixture } from '../test-fixtures.fixture'
import { isChatChannel, isChatMessage } from './rest'
import { decodeServerEvent } from './events'
import type { VoiceSnapshot } from './events'
import { ContractError } from './validation'

describe('application wire contracts', () => {
  it('distinguishes required nullable channel parents from omitted optional message references', () => {
    const channel = channelFixture()
    expect(isChatChannel(channel)).toBe(true)
    const withoutParent: Partial<typeof channel> = { ...channel }
    delete withoutParent.category_id
    expect(isChatChannel(withoutParent)).toBe(false)
    expect(isChatChannel({ ...channel, category_id: undefined })).toBe(false)
    const message = messageFixture()
    expect(isChatMessage(message)).toBe(true)
    expect(isChatMessage({ ...message, parent_id: null })).toBe(false)
    expect(isChatMessage({ ...message, parent_id: undefined })).toBe(false)
    expect(isChatMessage(messageFixture({ parent_id: fixtureId(8) }))).toBe(true)
  })

  it('validates a 250-member voice snapshot without dropping valid participants', () => {
    const room: VoiceSnapshot[string] = {}
    for (let index = 1; index <= 250; index++) {
      const id = fixtureId(index)
      room[id] = { ...userFixture({ id, username: `synthetic-${index}` }), joined_at: FIXTURE_TIMESTAMP, muted: false, deafened: false }
    }
    const event = decodeServerEvent({ type: 'voice_snapshot', payload: { [fixtureId(900)]: room } })
    expect(event?.type).toBe('voice_snapshot')
    if (event?.type !== 'voice_snapshot') throw new Error('Expected voice snapshot')
    expect(Object.keys(event.payload[fixtureId(900)] ?? {})).toHaveLength(250)
  })

  it('ignores future event names and rejects malformed known events without exposing payloads', () => {
    expect(decodeServerEvent({ type: 'future_extension', payload: { opaque: true } })).toBeNull()
    const marker = 'SYNTHETIC_PRIVATE_PAYLOAD'
    const malformed = { type: 'message_create', payload: { content: marker } }
    expect(() => decodeServerEvent(malformed)).toThrow(ContractError)
    try { decodeServerEvent(malformed) } catch (error: unknown) {
      expect(error).toBeInstanceOf(ContractError)
      if (!(error instanceof Error)) throw error
      expect(error.message).not.toContain(marker)
    }
    expect(() => decodeServerEvent({ type: 'voice_snapshot', payload: { arbitrary: {} } })).toThrow(ContractError)
  })
})
