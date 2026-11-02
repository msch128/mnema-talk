import type { ApiOptions } from '../lib/api'

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import { usePermissionsStore } from './permissions'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

const NOW = Date.parse('2026-11-02T12:00:00Z')
const base = { owner: false, permissions: 0, role_ids: [], top_position: 0, max_upload_mb: 50, catalog: [], channels: [] }

beforeEach(() => {
  setActivePinia(createPinia())
  vi.useFakeTimers({ now: NOW })
  apiMock.mockReset()
})
afterEach(() => vi.useRealTimers())

describe('permissions store, waits of a new member', () => {
  it('knows how long a new member still waits per action', async () => {
    const permissions = usePermissionsStore()
    expect(permissions.newMemberWait('write')).toBe(0)
    permissions.info = { ...base, new_member: { write_at: '2026-11-02T12:10:00Z' } }
    await nextTick()
    expect(permissions.newMemberWait('write')).toBe(600_000)
    expect(permissions.newMemberWait('react')).toBe(0)
    await vi.advanceTimersByTimeAsync(40_000)
    expect(permissions.newMemberWait('write')).toBe(560_000)
    permissions.reset()
  })

  it('fetches the permissions again when the first wait is over', async () => {
    apiMock.mockResolvedValue({ ...base })
    const permissions = usePermissionsStore()
    permissions.info = { ...base, new_member: { write_at: '2026-11-02T12:01:00Z', speak_at: '2026-11-02T13:00:00Z' } }
    await nextTick()
    await vi.advanceTimersByTimeAsync(59_000)
    expect(apiMock).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(2_000)
    expect(apiMock).toHaveBeenCalledWith('/api/permissions', expect.anything())
    expect(permissions.newMemberWait('write')).toBe(0)
    permissions.reset()
  })
})
