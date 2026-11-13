import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useChatStore } from './chat'
import { messageFixture, fixtureId } from '../test-fixtures.fixture'
import { noteServerTime, resetServerTime } from '../shared/lib/serverTime'

// A message with a lifetime leaves the open lists at its end on the client's
// own clock, as if its message_delete had come (doc/timed-messages.md): the
// server's sweep may be a moment late.

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date', 'performance', 'setTimeout', 'clearTimeout', 'setInterval', 'clearInterval'] })
  vi.setSystemTime(Date.parse('2026-11-13T10:00:00Z'))
  setActivePinia(createPinia())
  resetServerTime()
  // What a removal refreshes (member counts, pins) answers empty.
  vi.stubGlobal('fetch', vi.fn<typeof fetch>(async () => new Response('[]', { status: 200, headers: { 'Content-Type': 'application/json' } })))
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('messages with a lifetime in the open lists', () => {
  it('never end before the server says, on a device whose clock runs ahead', async () => {
    // The device is 30 s ahead of the server; the message was written now on the server's clock.
    vi.setSystemTime(Date.parse('2026-11-13T10:00:30Z'))
    const chat = useChatStore()
    const m = messageFixture({ id: fixtureId(30), channel_id: fixtureId(1), created_at: '2026-11-13T10:00:00Z', expires_at: '2026-11-13T10:01:00Z' })
    chat.messages = [m]
    await vi.advanceTimersByTimeAsync(35_000)
    expect(chat.messages.map(x => x.id), 'ended on the device clock, 25 s early').toEqual([m.id])
    await vi.advanceTimersByTimeAsync(26_000)
    expect(chat.messages).toEqual([])
  })

  it('leave the channel list and the thread at their end, and nothing else does', async () => {
    noteServerTime(Date.now()) // the server's clock, as voice_rooms or a Date header tells it
    const chat = useChatStore()
    const ch = fixtureId(1)
    const soon = messageFixture({ id: fixtureId(10), channel_id: ch, expires_at: '2026-11-13T10:00:05Z' })
    const later = messageFixture({ id: fixtureId(11), channel_id: ch, expires_at: '2026-11-13T11:00:00Z' })
    const plain = messageFixture({ id: fixtureId(12), channel_id: ch })
    chat.messages = [soon, later, plain]
    await vi.advanceTimersByTimeAsync(4000)
    expect(chat.messages.map(m => m.id)).toEqual([soon.id, later.id, plain.id])
    await vi.advanceTimersByTimeAsync(1500)
    expect(chat.messages.map(m => m.id)).toEqual([later.id, plain.id])

    // A reply in an open thread goes the same way, and a later list keeps its timer.
    const reply = messageFixture({ id: fixtureId(13), channel_id: ch, parent_id: plain.id, expires_at: '2026-11-13T10:00:30Z' })
    chat.threadReplies.push(reply)
    await vi.advanceTimersByTimeAsync(30_000)
    expect(chat.threadReplies.map(m => m.id)).toEqual([])
    await vi.advanceTimersByTimeAsync(3600_000)
    expect(chat.messages.map(m => m.id)).toEqual([plain.id])
  })

  it('removes at once what has ended already when a list arrives', async () => {
    noteServerTime(Date.now())
    const chat = useChatStore()
    const ended = messageFixture({ id: fixtureId(20), channel_id: fixtureId(1), expires_at: '2026-11-13T09:59:00Z' })
    chat.messages = [ended, messageFixture({ id: fixtureId(21), channel_id: fixtureId(1) })]
    await vi.advanceTimersByTimeAsync(0)
    expect(chat.messages.map(m => m.id)).toEqual([fixtureId(21)])
  })
})
