import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ref } from 'vue'
import { statePath, syncSettings } from './useAppRouting'
import { currentRoute, parseRoute } from '../lib/router'
import { settingsTab } from '../lib/settingsDialog'
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

  it('goes back to the forum address once the post is closed', () => {
    const forum = { id: 'f1', type: 'forum', slug: 'ideas' }
    expect(statePath(ctxWith({ active: forum, thread: null }), null)).toBe('/c/ideas')
  })

  it('has no address for nothing, or for a channel that is gone', () => {
    expect(statePath(ctxWith({}), null)).toBe('/')
    expect(statePath(ctxWith({ active: { id: 'gone', type: 'text' } }), null)).toBe('/')
    expect(statePath(ctxWith({ activeView: 'voice' }), 'gone')).toBe('/')
  })
})

describe('the address of the settings dialog', () => {
  const dialog = (open: boolean) => ({ account: { dialogOpen: open }, currentStatePath: () => '/c/general' }) as unknown as RoutingContext

  beforeEach(() => {
    window.history.replaceState({ idx: 0 }, '', '/c/general')
    currentRoute.value = parseRoute('/c/general')
    settingsTab.value = 'audio'
  })

  it('writes /settings/:tab when the dialog opens and replaces it when the section changes', () => {
    expect(syncSettings(dialog(true))).toBe(true)
    expect(window.location.pathname).toBe('/settings/audio')
    expect(window.history.state).toEqual({ idx: 1 })
    settingsTab.value = 'privacy'
    expect(syncSettings(dialog(true))).toBe(true)
    expect(window.location.pathname).toBe('/settings/privacy')
    expect(window.history.state).toEqual({ idx: 1 })
  })

  it('goes back to where it was opened, so Back and the close button agree', () => {
    syncSettings(dialog(true))
    const back = vi.spyOn(window.history, 'back').mockImplementation(() => {})
    expect(syncSettings(dialog(false))).toBe(true)
    expect(back).toHaveBeenCalledTimes(1)
    back.mockRestore()
  })

  it('leaves a settings address that was the first entry for the page underneath', () => {
    window.history.replaceState({ idx: 0 }, '', '/settings/audio')
    currentRoute.value = parseRoute('/settings/audio')
    expect(syncSettings(dialog(false))).toBe(true)
    expect(window.location.pathname).toBe('/c/general')
  })

  it('does nothing for a closed dialog away from its address', () => {
    expect(syncSettings(dialog(false))).toBe(false)
    expect(window.location.pathname).toBe('/c/general')
  })
})
