import type { ApiOptions } from '../lib/api'

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import AdminAccessSettings from './AdminAccessSettings.vue'
import { setLocale } from '../i18n'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

const settings = { community_name: 'Home', welcome_text: 'Hi', rules_text: '', rules_version: 0, start_channel_id: null, direct_messages_enabled: true, session_inactivity_days: 7 }

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset().mockResolvedValue(settings)
})

describe('AdminAccessSettings', () => {
  it('refuses days outside 1 to 365', async () => {
    const w = mount(AdminAccessSettings)
    await flushPromises()
    for (const bad of ['0', '366']) {
      await w.get('[data-testid="inactivity-days"]').setValue(bad)
      expect(w.get('[data-testid="access-save"]').attributes('disabled')).toBeDefined()
      expect(w.get('[role="alert"]').text()).toContain('1 to 365')
    }
  })

  it('saves on top of the texts the server holds right now, not a stale copy', async () => {
    const w = mount(AdminAccessSettings)
    await flushPromises()
    await w.get('[data-testid="inactivity-days"]').setValue('30')
    apiMock.mockReset()
    apiMock.mockResolvedValueOnce({ ...settings, welcome_text: 'Hi, edited elsewhere' })
    apiMock.mockResolvedValueOnce({ ...settings, session_inactivity_days: 30 })
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/admin/onboarding', expect.objectContaining({
      method: 'PUT',
      json: expect.objectContaining({ welcome_text: 'Hi, edited elsewhere', session_inactivity_days: 30, require_reacceptance: false })
    }))
    expect(w.get<HTMLInputElement>('[data-testid="inactivity-days"]').element.value).toBe('30')
  })
})
