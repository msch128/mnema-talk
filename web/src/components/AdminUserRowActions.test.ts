import { nextTick } from 'vue'
import { useCompactLayout } from '../composables/useCompactLayout'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import AdminUserRow from './AdminUserRow.vue'
import { usePermissionsStore } from '../stores/permissions'
import { useVoiceStore } from '../stores/voice'
import { useAuthStore } from '../stores/auth'
import { fixtureId, userFixture } from '../test-fixtures.fixture'
import { setLocale } from '../i18n'
import { member } from './adminTestkit.fixture'
import type { AdminUser } from '../types/domain'

vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: vi.fn(() => Promise.resolve(undefined))
}))

enableAutoUnmount(afterEach)
afterEach(() => { document.body.innerHTML = ''; vi.unstubAllGlobals(); useCompactLayout().isCompact.value = false })
beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  useAuthStore().user = userFixture({ id: fixtureId(1), role: 'admin' })
  usePermissionsStore().info = { owner: true, permissions: 0, max_upload_mb: 50, role_ids: [], top_position: 9, catalog: [], channels: [] }
})

const row = (user: AdminUser = member()) => mount(AdminUserRow, { props: { user }, global: { stubs: { UserAvatar: true } } })

describe('admin user row actions for the owner', () => {
  it('each icon asks the table for its action', async () => {
    const w = row({ ...member(), login_locked: true })
    for (const id of ['clear-lockout', 'roles', 'disable', 'reset-password', 'reset-two-factor', 'revoke-sessions']) {
      await w.get(`[data-testid="action-${id}"]`).trigger('click')
    }
    expect(Object.keys(w.emitted()).filter(k => k !== 'click').sort()).toEqual(
      ['clear-lockout', 'reset-login', 'reset-two-factor', 'revoke-sessions', 'roles', 'toggle-disable'])
  })

  it('shows a locked account as locked', () => {
    expect(row({ ...member(), login_locked: true }).find('[data-testid="user-login-locked"]').exists()).toBe(true)
  })

  it('offers kick from voice for a member who sits in a Talk, and shows where', async () => {
    const u = member()
    useVoiceStore().channelUsers = { c1: { [u.id]: {} } } as never
    const w = row(u)
    expect(w.text()).toContain('c1')
    await w.get('[data-testid="action-kick"]').trigger('click')
    expect(w.emitted('kick')).toHaveLength(1)
  })

  it('falls back to the channel id when the room is unknown', () => {
    const u = member()
    useVoiceStore().channelUsers = { c9: { [u.id]: {} } } as never
    expect(row(u).text()).toContain('c9')
  })

  it('shows a disabled account with the enable action', () => {
    const w = row(member(true))
    expect(w.get('[data-testid="user-status"]').text()).toBe('Disabled')
    expect(w.get('[data-testid="action-disable"]').attributes('aria-label')).toBe('Enable account')
  })

  it('shows roles by name and the community owner as Owner', () => {
    expect(row({ ...member(), role: 'admin' } as AdminUser).get('[data-testid="user-role"]').text()).toBe('Owner')
  })

  it('shows Never for an account that has not been seen yet', () => {
    expect(row({ ...member(), last_seen_at: null } as AdminUser).get('[data-testid="user-lastseen"]').text()).toBe('Never')
  })

  it('on a phone the labelled menu offers the same actions and each asks the table', async () => {
    vi.stubGlobal('matchMedia', (query: string) => ({ matches: true, media: query, addEventListener: () => {}, removeEventListener: () => {} }))
    useCompactLayout().isCompact.value = true
    const u = { ...member(), login_locked: true }
    useVoiceStore().channelUsers = { c1: { [u.id]: {} } } as never
    const w = row(u)
    const seen: string[] = []
    for (let i = 0; i < 7; i++) {
      await w.get('[data-testid="row-actions"]').trigger('click')
      await nextTick(); await nextTick()
      const items = [...document.querySelectorAll<HTMLElement>('[role="menu"] [role="menuitem"]')]
      seen.push(items[i]?.textContent?.trim() ?? '')
      items[i]?.click()
      await nextTick()
    }
    expect(seen.filter(Boolean)).toHaveLength(7)
    expect(Object.keys(w.emitted()).filter(k => k !== 'click').sort()).toEqual(
      ['clear-lockout', 'kick', 'reset-login', 'reset-two-factor', 'revoke-sessions', 'roles', 'toggle-disable'])
  })
})
