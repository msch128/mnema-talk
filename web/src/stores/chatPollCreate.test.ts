import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useChatStore } from './chat'
import { useEmojiStore } from './emoji'
import { fixtureId, messageFixture } from '../test-fixtures.fixture'

const EMOJI_ID = '11111111-2222-4333-8444-555555555555'
let sent: { question: string; options: string[] } | undefined

beforeEach(() => {
  setActivePinia(createPinia())
  sent = undefined
  vi.stubGlobal('fetch', vi.fn(async (_url: string, init?: RequestInit) => {
    sent = JSON.parse(String(init?.body))
    return Response.json(messageFixture({ id: fixtureId(7), channel_id: fixtureId(2), content: 'q' }))
  }))
})
afterEach(() => { vi.unstubAllGlobals() })

describe('starting a poll', () => {
  it('turns a typed :name: in the question into the emoji, and leaves the options plain', async () => {
    useEmojiStore().items = [{ id: EMOJI_ID, name: 'blob', animated: false, url: '/e' }]
    await useChatStore().createPoll(fixtureId(2), { question: 'Pizza :blob: or pasta?', options: [':blob:', 'Pasta'] })
    expect(sent?.question).toBe(`Pizza <:blob:${EMOJI_ID}> or pasta?`)
    expect(sent?.options).toEqual([':blob:', 'Pasta'])
  })
})
