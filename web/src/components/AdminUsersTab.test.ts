import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import type { ApiOptions } from '../lib/api'
import AdminUsersTab from './AdminUsersTab.vue'
import BaseDialog from './BaseDialog.vue'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'
import { channelFixture, fixtureId, userFixture, voiceUserFixture } from '../test-fixtures.fixture'
import { member, errors, button } from './adminTestkit.fixture'
import { useAuthStore } from '../stores/auth'
import { setLocale } from '../i18n'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (original) => ({
  ...(await original<typeof import('../lib/api')>()),
  api: (path: string, options?: ApiOptions) => apiMock(path, options),
}))
const confirmMock = vi.fn<() => Promise<boolean>>()
vi.mock('../lib/confirm', () => ({ confirm: () => confirmMock() }))
// The live operations panel has its own tests and endpoint.
vi.mock('./AdminOpsPanel.vue', () => ({ default: { template: '<div />' } }))
vi.mock('./AdminUploadLimit.vue', () => ({ default: { template: '<div />' } }))

let wrappers: VueWrapper[] = []
function keep<T extends VueWrapper>(w: T): T {
  wrappers.push(w)
  return w
}
beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  confirmMock.mockReset().mockResolvedValue(true)
  useAuthStore().user = userFixture({ role: 'admin', username: 'owner' }) // the Owner may do everything
})
afterEach(() => {
  for (const w of wrappers) w.unmount()
  wrappers = []
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  vi.useRealTimers()
  document.body.innerHTML = ''
})
describe('admin account recovery and cancellation', () => {
  async function openUsers() {
    apiMock.mockResolvedValue([member(), member(true)])
    const w = keep(mount(AdminUsersTab))
    await flushPromises()
    return w
  }
  it.each(['action-disable', 'action-revoke-sessions', 'action-kick'])(
    'cancels %s without a mutation',
    async (action) => {
      useVoiceStore().channelUsers = {
        [fixtureId(2)]: {
          [fixtureId(41)]: voiceUserFixture({ id: fixtureId(41) }),
        },
      }
      const w = await openUsers()
      confirmMock.mockResolvedValue(false)
      apiMock.mockClear()
      await w.find(`[data-testid="${action}"]`).trigger('click')
      await flushPromises()
      expect(apiMock).not.toHaveBeenCalled()
    },
  )
  it.each(['action-disable', 'action-revoke-sessions', 'action-kick'])(
    'reports failed %s and preserves the table',
    async (action) => {
      useVoiceStore().channelUsers = {
        [fixtureId(2)]: {
          [fixtureId(41)]: voiceUserFixture({ id: fixtureId(41) }),
        },
      }
      useChatStore().uncategorized = [channelFixture({ name: 'Lounge' })]
      const w = await openUsers()
      expect(w.text()).toContain('Lounge')
      apiMock.mockRejectedValueOnce(new Error('mutation denied'))
      await w.find(`[data-testid="${action}"]`).trigger('click')
      await flushPromises()
      expect(errors()).toContain('mutation denied')
      expect(w.findAll('[data-testid="user-row"]')).toHaveLength(2)
    },
  )
  it('reports enable and password failures, preserves input for retry and permits cancelling', async () => {
    const w = await openUsers()
    apiMock.mockRejectedValueOnce(new Error('enable failed'))
    await w.findAll('[data-testid="action-disable"]')[1]!.trigger('click')
    await flushPromises()
    expect(errors()).toContain('enable failed')
    await w.find('[data-testid="action-reset-password"]').trigger('click')
    await w.find('[data-testid="new-password-input"]').setValue('long-password')
    apiMock.mockRejectedValueOnce(new Error('reset failed'))
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(errors()).toContain('reset failed')
    expect(w.find('[data-testid="new-password-input"]').element).toHaveProperty(
      'value',
      'long-password',
    )
    await button(w, 'Cancel').trigger('click')
    expect(w.find('form').exists()).toBe(false)
    await w.find('[data-testid="action-reset-password"]').trigger('click')
    w.findComponent(BaseDialog).vm.$emit('close')
    await nextTick()
    expect(w.find('form').exists()).toBe(false)
  })
  it('filters by username, handles no matches, and keeps members after a failed refresh', async () => {
    const w = await openUsers()
    await w.find('input[type="search"]').setValue(' MEMBER ')
    expect(w.findAll('[data-testid="user-row"]')).toHaveLength(1)
    await w.find('input[type="search"]').setValue('absent')
    expect(w.findAll('[data-testid="user-row"]')).toHaveLength(0)
    await w.find('input[type="search"]').setValue('')
    apiMock.mockRejectedValueOnce(new Error('reload failed'))
    await w.find('button[aria-label="Refresh"]').trigger('click')
    await flushPromises()
    expect(w.findAll('[data-testid="user-row"]')).toHaveLength(2)
    expect(errors()).toContain('reload failed')
  })
  it('falls back to original last-seen text if locale formatting throws', async () => {
    vi.spyOn(Date.prototype, 'toLocaleString').mockImplementation(() => {
      throw new RangeError('locale')
    })
    const w = await openUsers()
    expect(w.text()).toContain('2026-01-01T00:00:00Z')
  })
})
