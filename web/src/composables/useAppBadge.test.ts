import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, nextTick, type EffectScope } from 'vue'
import { createPinia, setActivePinia } from 'pinia'

const badge = vi.hoisted(() => vi.fn())
vi.mock('../lib/platform', async importOriginal => {
  const real = await importOriginal<typeof import('../lib/platform')>()
  return { ...real, platform: { ...real.platform, setBadge: badge } }
})

import { useAppBadge } from './useAppBadge'
import { useChatStore } from '../stores/chat'

let scope: EffectScope
beforeEach(() => { setActivePinia(createPinia()); badge.mockClear() })
afterEach(() => scope.stop())

describe('useAppBadge', () => {
  it('shows the unread mentions of unmuted channels and clears at zero', async () => {
    const chat = useChatStore()
    scope = effectScope()
    scope.run(() => useAppBadge())
    expect(badge).toHaveBeenLastCalledWith(0)
    chat.readStates = {
      a: { channel_id: 'a', notify_level: 'all', unread_count: 5, mention_count: 2, last_read_at: null },
      b: { channel_id: 'b', notify_level: 'mute', unread_count: 5, mention_count: 7, last_read_at: null },
      c: { channel_id: 'c', notify_level: 'mentions', unread_count: 1, mention_count: 1, last_read_at: null },
    }
    await nextTick()
    expect(badge).toHaveBeenLastCalledWith(3)
    chat.readStates = {}
    await nextTick()
    expect(badge).toHaveBeenLastCalledWith(0)
  })
})
