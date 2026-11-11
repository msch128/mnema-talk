import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import AdminUserRow from './AdminUserRow.vue'
import AdminChannelRow from './AdminChannelRow.vue'
import AdminCategoryHeader from './AdminCategoryHeader.vue'
import { useCompactLayout } from '../../shared/composables/useCompactLayout'
import { usePermissionsStore } from '../../stores/permissions'
import { setLocale } from '../../i18n'
import { member } from './adminTestkit.fixture'
import type { Category, Channel } from '../../types/domain'

// A phone: below 768px the icon-only row actions have no tooltip, so each row offers one "..." button
// that opens the same actions as a labelled menu.
beforeEach(() => {
  vi.stubGlobal('matchMedia', (query: string) => ({ matches: true, media: query, addEventListener: () => {}, removeEventListener: () => {} }))
  setActivePinia(createPinia())
  setLocale('en')
  useCompactLayout().isCompact.value = true
})
afterEach(() => {
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
})

const menuLabels = () => [...document.querySelectorAll('[role="menu"] [role="menuitem"]')].map(el => el.textContent?.trim())

async function open(w: ReturnType<typeof mount>) {
  await w.get('[data-testid="row-actions"]').trigger('click')
  await nextTick()
  await nextTick()
}

function userRow(timeoutUntil?: string) {
  const permissions = usePermissionsStore()
  vi.spyOn(permissions, 'can').mockReturnValue(true)
  vi.spyOn(permissions, 'outranksMember').mockReturnValue(true)
  Object.defineProperty(permissions, 'isOwner', { value: true })
  const user = { ...member(), id: 'u-2', username: 'mira', display_name: 'Mira', role: 'user' as const, ...(timeoutUntil ? { timeout_until: timeoutUntil } : {}) }
  const w = mount(AdminUserRow, { props: { user }, attachTo: document.body, global: { stubs: { UserAvatar: true } } })
  return w
}

describe('admin user row on a phone', () => {
  it('replaces the five icons with one button named after the member', () => {
    const w = userRow()
    expect(w.findAll('[data-testid^="action-"]')).toHaveLength(0)
    expect(w.get('[data-testid="row-actions"]').attributes('aria-label')).toBe('Actions for Mira')
  })

  it('lists every action by name and runs the one that is chosen', async () => {
    const w = userRow()
    await open(w)
    expect(menuLabels()).toEqual(expect.arrayContaining(['Roles', 'Disable account', 'Reset password', 'Reset two-factor sign-in', 'End sessions']))
    const revoke = [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(el => /End sessions/.test(el.textContent ?? ''))
    revoke?.click()
    expect(w.emitted('revoke-sessions')).toHaveLength(1)
  })

  it('offers End timeout in the menu for a timed-out member, and only then', async () => {
    const w = userRow(new Date(Date.now() + 3_600_000).toISOString())
    await open(w)
    expect(menuLabels()).toContain('End timeout')
    document.body.innerHTML = ''
    const plain = userRow()
    await open(plain)
    expect(menuLabels()).not.toContain('End timeout')
  })

  it('names the last-seen column inline, since the heading is gone', () => {
    expect(userRow().text()).toContain('Last seen: ')
  })

  it('keeps the icons on a wide screen', () => {
    useCompactLayout().isCompact.value = false
    const w = userRow()
    expect(w.find('[data-testid="row-actions"]').exists()).toBe(false)
    expect(w.find('[data-testid="action-revoke-sessions"]').exists()).toBe(true)
  })
})

describe('layout editor rows on a phone', () => {
  const channel = { id: 'c1', name: 'general', type: 'text' } as Channel
  const category = { id: 'k1', name: 'Lounge', channels: [] } as unknown as Category

  it('offers a channel its actions in a labelled menu', async () => {
    const w = mount(AdminChannelRow, { props: { channel, index: 0, count: 2 }, attachTo: document.body })
    expect(w.find('[data-testid="rename-channel"]').exists()).toBe(false)
    await open(w)
    expect(menuLabels()).toEqual(['Move general up', 'Move general down', 'Edit channel general', 'Delete channel general'])
    ;[...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(el => /Delete/.test(el.textContent ?? ''))?.click()
    expect(w.emitted('delete')).toHaveLength(1)
  })

  it('offers a category its actions in a labelled menu', async () => {
    const w = mount(AdminCategoryHeader, { props: { category, index: 0, count: 1 }, attachTo: document.body })
    await open(w)
    expect(menuLabels()).toHaveLength(5)
    ;[...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(el => /Edit category/.test(el.textContent ?? ''))?.click()
    expect(w.emitted('rename')).toHaveLength(1)
  })
})
