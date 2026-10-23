import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, nextTick, type EffectScope } from 'vue'
import { createPinia, setActivePinia } from 'pinia'

const badge = vi.hoisted(() => vi.fn())
const report = vi.hoisted(() => vi.fn())
const host = vi.hoisted(() => ({ reports: false }))
vi.mock('../lib/platform', async importOriginal => {
  const real = await importOriginal<typeof import('../lib/platform')>()
  return { ...real, platform: { ...real.platform, setBadge: badge, reportUnread: report, supportsUnreadReport: () => host.reports } }
})

import { useAppBadge } from './useAppBadge'
import { useChatStore } from '../stores/chat'

let scope: EffectScope
beforeEach(() => { setActivePinia(createPinia()); badge.mockClear(); report.mockClear(); host.reports = false })
afterEach(() => scope.stop())

describe('useAppBadge', () => {
  it('shows the unread mentions, as the tab title does, and clears at zero', async () => {
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
    expect(badge).toHaveBeenLastCalledWith(10)
    chat.readStates = {}
    await nextTick()
    expect(badge).toHaveBeenLastCalledWith(0)
  })

  it('reports unread channels and mentions to a multi-instance host instead of the plain badge', async () => {
    host.reports = true
    const chat = useChatStore()
    scope = effectScope()
    scope.run(() => useAppBadge())
    expect(report).toHaveBeenLastCalledWith({ unread: 0, mentions: 0 })
    chat.readStates = {
      a: { channel_id: 'a', notify_level: 'all', unread_count: 5, mention_count: 2, last_read_at: null },
      b: { channel_id: 'b', notify_level: 'mute', unread_count: 5, mention_count: 0, last_read_at: null },
    }
    await nextTick()
    expect(report).toHaveBeenLastCalledWith({ unread: 1, mentions: 2 })
    const calls = report.mock.calls.length
    chat.readStates = { ...chat.readStates }
    await nextTick()
    expect(report.mock.calls.length).toBe(calls)
    expect(badge).not.toHaveBeenCalled()
  })
})
