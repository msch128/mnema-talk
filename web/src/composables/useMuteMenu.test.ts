import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useChatStore } from '../stores/chat'
import type { ContextMenuItem } from '../components/menuTypes'
import { t } from '../i18n'
import { categoryFixture, channelFixture } from '../test-fixtures.fixture'
import { formatMuteEnd, MUTE_DURATIONS, notifyMenuItems, storedSetting } from './useMuteMenu'

const NOW = new Date('2026-01-01T12:00:00Z')

function submenu(items: ContextMenuItem[]) {
  const found = items.find(i => i.type === 'submenu')
  if (!found || found.type !== 'submenu') throw new Error('no submenu')
  return found
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(NOW)
  setActivePinia(createPinia())
})
afterEach(() => vi.useRealTimers())

describe('stored setting', () => {
  it('is inherit when nothing is stored', () => {
    expect(storedSetting({ kind: 'channel', id: 'x' })).toEqual({ level: 'inherit', muted_until: null })
    expect(storedSetting({ kind: 'category', id: 'x' })).toEqual({ level: 'inherit', muted_until: null })
  })

  it('reads channel and category settings apart', () => {
    const chat = useChatStore()
    chat.notifySettings = {
      channels: [{ channel_id: 'x', level: 'mentions', muted_until: null }],
      categories: [{ category_id: 'x', level: 'mute', muted_until: '2026-01-01T13:00:00Z' }],
      server: { level: 'all', muted_until: null, suppress_everyone: false }
    }
    expect(storedSetting({ kind: 'channel', id: 'x' }).level).toBe('mentions')
    expect(storedSetting({ kind: 'category', id: 'x' })).toEqual({ level: 'mute', muted_until: '2026-01-01T13:00:00Z' })
  })
})

describe('formatMuteEnd', () => {
  it('gives a time today and a date with the time on another day', () => {
    const today = formatMuteEnd(new Date(NOW.getTime() + 3_600_000).toISOString(), NOW)
    const later = formatMuteEnd(new Date(NOW.getTime() + 3 * 86_400_000).toISOString(), NOW)
    expect(today).toMatch(/\d/)
    expect(later.length).toBeGreaterThan(today.length)
  })
})

describe('formatMuteEnd locale', () => {
  it('follows the given language, not the browser', () => {
    const iso = '2026-01-04T14:30:00Z'
    expect(formatMuteEnd(iso, NOW, 'en-US')).not.toEqual(formatMuteEnd(iso, NOW, 'de'))
    expect(formatMuteEnd(iso, NOW, 'de')).toContain('04.01.26')
  })
})

describe('what "my default" means', () => {
  const label = (target: Parameters<typeof notifyMenuItems>[0]) => notifyMenuItems(target).find(i => i.id === 'notify-inherit')?.label

  it('names the server-wide level, and the category level for a channel in a category that has one', () => {
    const chat = useChatStore()
    chat.categories = [categoryFixture({ id: 'k1', name: 'Text', channels: [channelFixture({ id: 'c1', category_id: 'k1' })] })]
    chat.notifySettings = { channels: [], categories: [], server: { level: 'mentions', muted_until: null, suppress_everyone: false } }
    expect(label({ kind: 'channel', id: 'c1' })).toBe(t('notifications.inherit', { level: t('notifications.mentions') }))
    expect(label({ kind: 'category', id: 'k1' })).toBe(t('notifications.inherit', { level: t('notifications.mentions') }))
    chat.notifySettings = { channels: [], categories: [{ category_id: 'k1', level: 'mute', muted_until: null }], server: { level: 'mentions', muted_until: null, suppress_everyone: false } }
    expect(label({ kind: 'channel', id: 'c1' })).toBe(t('notifications.inherit', { level: t('notifications.bellNothing') }))
    // The category itself and a channel outside any category follow the server.
    expect(label({ kind: 'category', id: 'k1' })).toBe(t('notifications.inherit', { level: t('notifications.mentions') }))
    expect(label({ kind: 'channel', id: 'c1', inCategory: false })).toBe(t('notifications.inherit', { level: t('notifications.mentions') }))
  })
})

describe('notification menu', () => {
  it('offers the levels and every duration, with category wording for a category', () => {
    const channel = notifyMenuItems({ kind: 'channel', id: 'c1' })
    const category = notifyMenuItems({ kind: 'category', id: 'k1' })
    expect(channel.filter(i => i.type === 'radio').map(i => i.id)).toEqual(['notify-inherit', 'notify-all', 'notify-mentions', 'notify-nothing'])
    // "Nothing" is the open-ended mute, so the "Mute for…" list only has the timed ones.
    expect(submenu(channel).items.map(i => i.id)).toEqual(MUTE_DURATIONS.filter(d => d.ms !== null).map(d => `mute-${d.id}`))
    const label = (items: ContextMenuItem[]) => items.find(i => i.id === 'notify-inherit')?.label
    // Nothing is set anywhere: "my default" is the server's, all messages.
    expect(label(channel)).toBe(t('notifications.inherit', { level: t('notifications.all') }))
    expect(label(category)).toBe(label(channel))
    expect(label(notifyMenuItems({ kind: 'channel', id: 'c2', inCategory: false }))).toBe(label(category))
    expect(channel.find(i => i.id === 'notify-unmute')).toBeUndefined()
  })

  it('sends a timed mute with an end, and "until I turn it back on" without one', () => {
    const chat = useChatStore()
    const set = vi.spyOn(chat, 'setNotificationLevel').mockResolvedValue(true)
    const setCategory = vi.spyOn(chat, 'setCategoryNotificationLevel').mockResolvedValue(true)
    const items = submenu(notifyMenuItems({ kind: 'channel', id: 'c1' })).items
    items[0]!.action?.()
    expect(set).toHaveBeenLastCalledWith('c1', 'mute', new Date(NOW.getTime() + 15 * 60_000).toISOString())
    items[3]!.action?.()
    expect(set).toHaveBeenLastCalledWith('c1', 'mute', new Date(NOW.getTime() + 24 * 3_600_000).toISOString())
    notifyMenuItems({ kind: 'channel', id: 'c1' }).find(i => i.id === 'notify-nothing')!.action?.()
    expect(set).toHaveBeenLastCalledWith('c1', 'mute', null)
    submenu(notifyMenuItems({ kind: 'category', id: 'k1' })).items[1]!.action?.()
    expect(setCategory).toHaveBeenCalledWith('k1', 'mute', new Date(NOW.getTime() + 3_600_000).toISOString())
  })

  it('shows when a mute ends and offers to unmute', () => {
    const chat = useChatStore()
    const set = vi.spyOn(chat, 'setNotificationLevel').mockResolvedValue(true)
    chat.notifySettings = { channels: [{ channel_id: 'c1', level: 'mute', muted_until: '2026-01-01T13:00:00Z' }], categories: [], server: { level: 'all', muted_until: null, suppress_everyone: false } }
    const items = notifyMenuItems({ kind: 'channel', id: 'c1' })
    expect(submenu(items).value).toContain(formatMuteEnd('2026-01-01T13:00:00Z', NOW))
    items.find(i => i.id === 'notify-unmute')!.action?.()
    expect(set).toHaveBeenCalledWith('c1', 'inherit', null)

    chat.notifySettings = { channels: [{ channel_id: 'c1', level: 'mute', muted_until: null }], categories: [], server: { level: 'all', muted_until: null, suppress_everyone: false } }
    expect(submenu(notifyMenuItems({ kind: 'channel', id: 'c1' })).value).toBeTruthy()
  })
})
