import type { ApiOptions } from '../lib/api'

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useRaidStore } from './raid'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

const settings = { join_limit: 0, join_window_minutes: 10, joined_in_window: 0, min_age_write_minutes: 0, min_age_react_minutes: 0, min_age_speak_minutes: 0, lockdown: { active: true, pause_invites: false } }

beforeEach(() => {
  setActivePinia(createPinia())
  apiMock.mockReset()
})

describe('raid store', () => {
  it('loads, shows a running lockdown and forgets everything on reset', async () => {
    apiMock.mockResolvedValueOnce(settings)
    const raid = useRaidStore()
    await raid.load()
    expect(raid.lockdownActive).toBe(true)
    raid.reset()
    expect(raid.settings).toBeNull()
    expect(raid.lockdownActive).toBe(false)
  })

  it('remembers that loading failed', async () => {
    apiMock.mockRejectedValueOnce(new Error('down'))
    const raid = useRaidStore()
    await raid.load()
    expect(raid.failed).toBe(true)
  })
})
