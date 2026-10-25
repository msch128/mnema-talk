import { describe, it, expect } from 'vitest'
import { ref } from 'vue'
import { statePath } from './useAppRouting'
import type { RoutingContext } from './appRoutingApply'

const POST = '0f3a9c1e-1111-4222-8333-444455556666'
const DM = 'aaaaaaaa-0000-4000-8000-000000000001'

function ctxWith(state: { activeView?: string; active?: { id: string; type: string; slug?: string } | null; thread?: { id: string; title?: string } | null; chat?: boolean }) {
  const channels = [
    { id: 'c1', type: 'text', slug: 'general' },
    { id: 'f1', type: 'forum', slug: 'ideas' },
    { id: 'v1', type: 'voice', slug: 'lounge' },
  ]
  const chatStore = {
    allChannels: channels,
    activeChannel: state.active ?? null,
    activeThread: state.thread ?? null,
    isDm: (id: string) => id === DM,
    linkTo: (id: string, tail: { threadId?: string } = {}) => (id === DM ? '/dm/alice' : `/c/${channels.find(c => c.id === id)?.slug}${tail.threadId ? `/t/${tail.threadId}` : ''}`),
  }
  return { chatStore, voiceStore: { activeView: state.activeView ?? 'chat' }, voiceShowChat: ref(!!state.chat) } as unknown as RoutingContext
}

describe('the address of what is on screen', () => {
  it('names the channel, the thread, the Talk and its chat by slug', () => {
    expect(statePath(ctxWith({ active: { id: 'c1', type: 'text' } }), null)).toBe('/c/general')
    expect(statePath(ctxWith({ active: { id: 'c1', type: 'text' }, thread: { id: 't1' } }), null)).toBe('/c/general/t/t1')
    expect(statePath(ctxWith({ activeView: 'voice' }), 'v1')).toBe('/v/lounge')
    expect(statePath(ctxWith({ activeView: 'voice', chat: true }), 'v1')).toBe('/v/lounge/chat')
    expect(statePath(ctxWith({ active: { id: DM, type: 'text' } }), null)).toBe('/dm/alice')
  })

  it('gives a forum post its own address', () => {
    expect(statePath(ctxWith({ active: { id: 'f1', type: 'forum', slug: 'ideas' }, thread: { id: POST, title: 'New kitchen?' } }), null)).toBe('/c/ideas/p/new-kitchen-0f3a9c1e')
  })

  it('has no address for nothing, or for a channel that is gone', () => {
    expect(statePath(ctxWith({}), null)).toBe('/')
    expect(statePath(ctxWith({ active: { id: 'gone', type: 'text' } }), null)).toBe('/')
    expect(statePath(ctxWith({ activeView: 'voice' }), 'gone')).toBe('/')
  })
})
