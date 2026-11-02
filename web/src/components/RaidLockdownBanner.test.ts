import type { ApiOptions } from '../lib/api'

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import RaidLockdownBanner from './RaidLockdownBanner.vue'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { PERMISSION_BITS } from '../lib/permissions'
import { setLocale } from '../i18n'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

enableAutoUnmount(afterEach)

const settings = (active: boolean) => ({
  join_limit: 0, join_window_minutes: 10, joined_in_window: 0, min_age_write_minutes: 0, min_age_react_minutes: 0, min_age_speak_minutes: 0,
  lockdown: { active, pause_invites: false }
})

function asAdmin(on: boolean) {
  usePermissionsStore().info = {
    owner: false, permissions: on ? PERMISSION_BITS.administrator : 0, max_upload_mb: 50, role_ids: [], top_position: 0, catalog: [], channels: []
  }
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
})

const mountBanner = async () => {
  const w = mount(RaidLockdownBanner)
  await flushPromises()
  return w
}

describe('the lockdown banner', () => {
  it('tells an administrator the door is shut and ends the lockdown in one click', async () => {
    asAdmin(true)
    apiMock.mockResolvedValueOnce(settings(true))
    const w = await mountBanner()
    expect(w.get('[data-testid="lockdown-banner"]').text()).toContain('Lockdown is on: nobody new can sign up.')
    apiMock.mockResolvedValueOnce(settings(false))
    await w.get('[data-testid="lockdown-banner-end"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/admin/raid/lockdown', expect.objectContaining({ json: { active: false } }))
    expect(w.find('[data-testid="lockdown-banner"]').exists()).toBe(false)
  })

  it('names the time a lockdown ends by itself', async () => {
    asAdmin(true)
    apiMock.mockResolvedValueOnce({ ...settings(true), lockdown: { active: true, pause_invites: false, until: '2026-11-02T18:00:00Z' } })
    const w = await mountBanner()
    expect(w.get('[data-testid="lockdown-banner"]').text()).toContain('Lockdown is on until')
  })

  it('keeps the banner and says so when the lockdown cannot be ended', async () => {
    asAdmin(true)
    apiMock.mockResolvedValueOnce(settings(true))
    const w = await mountBanner()
    apiMock.mockRejectedValueOnce(new Error('down'))
    await w.get('[data-testid="lockdown-banner-end"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="lockdown-banner"]').exists()).toBe(true)
    expect(useToastStore().toasts.map(toast => toast.text)).toContain('down')
  })

  it('stays away without a lockdown and never asks a member who is not an administrator', async () => {
    asAdmin(true)
    apiMock.mockResolvedValueOnce(settings(false))
    expect((await mountBanner()).find('[data-testid="lockdown-banner"]').exists()).toBe(false)

    apiMock.mockReset()
    asAdmin(false)
    const member = await mountBanner()
    expect(apiMock).not.toHaveBeenCalled()
    expect(member.find('[data-testid="lockdown-banner"]').exists()).toBe(false)
  })
})
