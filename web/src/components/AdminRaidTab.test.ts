import type { ApiOptions } from '../lib/api'

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import AdminRaidTab from './AdminRaidTab.vue'
import { setLocale } from '../i18n'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

const off = { active: false, pause_invites: false }
const settings = {
  join_limit: 0, join_window_minutes: 10, joined_in_window: 3,
  min_age_write_minutes: 0, min_age_react_minutes: 0, min_age_speak_minutes: 0, lockdown: off
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset().mockResolvedValue(settings)
})

const mountTab = async () => {
  const w = mount(AdminRaidTab, { global: { directives: { tooltip: () => undefined } } })
  await flushPromises()
  return w
}

describe('the raid protection tab', () => {
  it('shows how many joined in the window and that sign-ups are open', async () => {
    const w = await mountTab()
    expect(w.get('[data-testid="raid-joined"]').text()).toBe('3 joined in the last 10 min.')
    expect(w.get('[data-testid="lockdown-state"]').text()).toBe('Sign-ups are open.')
  })

  it('starts a lockdown with the chosen options and ends it again', async () => {
    const w = await mountTab()
    await w.get('[data-testid="lockdown-pause-invites"]').setValue(true)
    await w.get('[data-testid="lockdown-hours"]').setValue('6')
    apiMock.mockResolvedValueOnce({ ...settings, lockdown: { active: true, pause_invites: true, until: '2026-11-02T18:00:00Z' } })
    await w.get('[data-testid="lockdown-start"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/admin/raid/lockdown', expect.objectContaining({
      method: 'PUT', json: { active: true, pause_invites: true, hours: 6 }
    }))
    expect(w.get('[data-testid="lockdown-state"]').text()).toContain('Lockdown is on until')
    expect(w.text()).toContain('Creating invites is paused too.')

    apiMock.mockResolvedValueOnce(settings)
    await w.get('[data-testid="lockdown-end"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/admin/raid/lockdown', expect.objectContaining({ json: { active: false, pause_invites: false, hours: 0 } }))
    expect(w.get('[data-testid="lockdown-state"]').text()).toBe('Sign-ups are open.')
  })

  it('saves the sign-up limit and the waiting times together', async () => {
    const w = await mountTab()
    await w.get('[data-testid="raid-limit"]').setValue('5')
    await w.get('[data-testid="raid-window"]').setValue('15')
    await w.get('[data-testid="raid-wait-write"]').setValue('60')
    await w.get('[data-testid="raid-wait-speak"]').setValue('1440')
    apiMock.mockResolvedValueOnce({ ...settings, join_limit: 5, join_window_minutes: 15, min_age_write_minutes: 60, min_age_speak_minutes: 1440 })
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/admin/raid', expect.objectContaining({
      method: 'PUT',
      json: { join_limit: 5, join_window_minutes: 15, min_age_write_minutes: 60, min_age_react_minutes: 0, min_age_speak_minutes: 1440 }
    }))
  })

  it('refuses a window outside 1 to 1440 minutes', async () => {
    const w = await mountTab()
    await w.get('[data-testid="raid-window"]').setValue('0')
    expect(w.get('[data-testid="raid-save"]').attributes('disabled')).toBeDefined()
    expect(w.get('[role="alert"]').text()).toContain('1440')
  })

  it('keeps a waiting time that is not one of the choices', async () => {
    apiMock.mockResolvedValue({ ...settings, min_age_react_minutes: 45 })
    const w = await mountTab()
    const values = w.findAll('[data-testid="raid-wait-react"] option').map(o => o.text())
    expect(values).toContain('45 min')
  })

  it('says when the settings cannot be loaded', async () => {
    apiMock.mockRejectedValue(new Error('down'))
    const w = await mountTab()
    expect(w.get('[role="alert"]').text()).toBe('Couldn’t load the raid protection settings.')
  })
})
