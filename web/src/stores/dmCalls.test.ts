import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useDmCallsStore, type CallInfo } from './dmCalls'
import type { DmCallEnd } from '../types/dmEvents'
import { fixtureId } from '../test-fixtures.fixture'

const conversation = fixtureId(100)
const talk = fixtureId(200)
const caller = fixtureId(2)
const me = fixtureId(1)

function info(overrides: Partial<CallInfo> = {}): CallInfo {
  return { conversationId: conversation, talkId: talk, messageId: fixtureId(300), callerId: caller, talkName: 'Talk 1', ...overrides }
}

function ended(overrides: Partial<DmCallEnd> = {}): DmCallEnd {
  return { conversation_id: conversation, talk_id: talk, reason: 'answered', active: true, ...overrides }
}

beforeEach(() => {
  vi.useFakeTimers()
  setActivePinia(createPinia())
})
afterEach(() => { useDmCallsStore().reset(); vi.useRealTimers() })

describe('a call that rings for me', () => {
  it('shows a card and marks the conversation as ringing', () => {
    const calls = useDmCallsStore()
    calls.ringIncoming(info(), 45, false)
    expect(calls.incoming).toHaveLength(1)
    expect(calls.incoming[0]).toMatchObject({ conversationId: conversation, talkId: talk, silent: false })
    expect(calls.markOf(conversation)).toBe('ringing')
    expect(calls.anyRinging).toBe(true)
  })

  it('goes away by itself when the ring is over, and the conversation stays marked as a call', () => {
    const calls = useDmCallsStore()
    calls.ringIncoming(info(), 45, false)
    vi.advanceTimersByTime(44_999)
    expect(calls.incoming).toHaveLength(1)
    vi.advanceTimersByTime(2)
    expect(calls.incoming).toEqual([])
    expect(calls.markOf(conversation)).toBe('active')
  })

  it('stops when it is answered on another device of mine, or by another member', () => {
    const calls = useDmCallsStore()
    calls.ringIncoming(info(), 45, false)
    calls.end(ended({ reason: 'answered', user_id: me }))
    expect(calls.incoming).toEqual([])
    expect(calls.markOf(conversation)).toBe('active')
    vi.advanceTimersByTime(60_000)
    expect(calls.incoming).toEqual([])
  })

  it('stops when the caller hangs up, and nothing marks the conversation any more', () => {
    const calls = useDmCallsStore()
    calls.ringIncoming(info(), 45, false)
    calls.end(ended({ reason: 'cancelled', active: false }))
    expect(calls.incoming).toEqual([])
    expect(calls.markOf(conversation)).toBeNull()
  })

  it('is closed by Dismiss without ending the call, and a repeated ring does not bring the card back', () => {
    const calls = useDmCallsStore()
    calls.ringIncoming(info(), 45, false)
    calls.dismiss(conversation)
    expect(calls.incoming).toEqual([])
    expect(calls.markOf(conversation)).toBe('active')
    calls.ringIncoming(info(), 30, false)
    expect(calls.incoming).toEqual([])
  })

  it('is over for the card when I join the Talk here', () => {
    const calls = useDmCallsStore()
    calls.ringIncoming(info(), 45, false)
    calls.joined(talk)
    expect(calls.incoming).toEqual([])
  })

  it('ignores the end of another Talk of the same conversation', () => {
    const calls = useDmCallsStore()
    calls.ringIncoming(info({ talkId: fixtureId(201) }), 45, false)
    calls.end(ended({ reason: 'cancelled', active: false }))
    expect(calls.incoming).toHaveLength(1)
  })

  it('shows the newest call first, one card per conversation', () => {
    const calls = useDmCallsStore()
    calls.ringIncoming(info(), 45, false)
    calls.ringIncoming(info({ conversationId: fixtureId(101), talkId: fixtureId(201) }), 45, true)
    calls.ringIncoming(info({ messageId: fixtureId(301) }), 45, false)
    expect(calls.incoming.map(c => c.conversationId)).toEqual([conversation, fixtureId(101)])
    expect(calls.incoming[0]?.messageId).toBe(fixtureId(301))
  })
})

describe('the call I started', () => {
  it('rings, then says nobody has joined when the ring runs out', () => {
    const calls = useDmCallsStore()
    calls.ringOutgoing(info({ callerId: me }), 45)
    expect(calls.outgoing?.phase).toBe('calling')
    expect(calls.markOf(conversation)).toBe('calling')
    vi.advanceTimersByTime(45_000)
    expect(calls.outgoing?.phase).toBe('nobody')
    expect(calls.markOf(conversation)).toBe('active')
  })

  it('goes away when somebody answers', () => {
    const calls = useDmCallsStore()
    calls.ringOutgoing(info({ callerId: me }), 45)
    calls.end(ended({ reason: 'answered', user_id: caller }))
    expect(calls.outgoing).toBeNull()
    expect(calls.markOf(conversation)).toBe('active')
    vi.advanceTimersByTime(60_000)
    expect(calls.outgoing).toBeNull()
  })

  it('says nobody has joined when the server times it out while I am still in the Talk', () => {
    const calls = useDmCallsStore()
    calls.ringOutgoing(info({ callerId: me }), 45)
    calls.end(ended({ reason: 'timeout', active: true }))
    expect(calls.outgoing?.phase).toBe('nobody')
  })

  it('is over when I cancel, or the server says it was cancelled', () => {
    const calls = useDmCallsStore()
    calls.ringOutgoing(info({ callerId: me }), 45)
    calls.clearOutgoing()
    expect(calls.outgoing).toBeNull()
    calls.ringOutgoing(info({ callerId: me }), 45)
    calls.end(ended({ reason: 'cancelled', active: false }))
    expect(calls.outgoing).toBeNull()
    expect(calls.markOf(conversation)).toBeNull()
  })
})

describe('the calls the conversation list names', () => {
  const listed = (call: Record<string, unknown> | undefined) => [{ id: conversation, call: call as never }]
  const ringing = { talk_id: talk, caller_id: caller, message_id: fixtureId(300), talk_name: 'Talk 1', ringing: true, ring_seconds: 20 }

  it('brings back a call that still rings, for the time it has left', () => {
    const calls = useDmCallsStore()
    const ring = vi.fn((call: CallInfo, seconds: number) => calls.ringIncoming(call, seconds, false))
    calls.syncList(listed(ringing), me, ring)
    expect(ring).toHaveBeenCalledWith(info(), 20, false)
    expect(calls.incoming).toHaveLength(1)
  })

  it('knows the call is mine when I am the caller', () => {
    const calls = useDmCallsStore()
    const ring = vi.fn()
    calls.syncList(listed({ ...ringing, caller_id: me }), me, ring)
    expect(ring).toHaveBeenCalledWith(expect.anything(), 20, true)
  })

  it('does not ring twice for a call the card already shows', () => {
    const calls = useDmCallsStore()
    calls.ringIncoming(info(), 45, false)
    const ring = vi.fn()
    calls.syncList(listed(ringing), me, ring)
    expect(ring).not.toHaveBeenCalled()
  })

  it('marks a call that does not ring and forgets what the list no longer has', () => {
    const calls = useDmCallsStore()
    calls.ringIncoming(info(), 45, false)
    calls.syncList(listed(undefined), me, vi.fn())
    expect(calls.incoming).toEqual([])
    expect(calls.markOf(conversation)).toBeNull()
    calls.syncList(listed({ ...ringing, ringing: false, ring_seconds: 0 }), me, vi.fn())
    expect(calls.markOf(conversation)).toBe('active')
  })

  it('drops the call I made when the list does not have it', () => {
    const calls = useDmCallsStore()
    calls.ringOutgoing(info({ callerId: me }), 45)
    calls.syncList([], me, vi.fn())
    expect(calls.outgoing).toBeNull()
  })
})

describe('leaving', () => {
  it('forgets everything, timers included', () => {
    const calls = useDmCallsStore()
    calls.ringIncoming(info(), 45, false)
    calls.ringOutgoing(info({ conversationId: fixtureId(101), callerId: me }), 45)
    calls.reset()
    expect(calls.incoming).toEqual([])
    expect(calls.outgoing).toBeNull()
    expect(calls.anyCall).toBe(false)
    vi.advanceTimersByTime(60_000)
    expect(calls.outgoing).toBeNull()
  })
})
