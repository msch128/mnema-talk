import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { usePermissionsStore } from './permissions'

beforeEach(() => setActivePinia(createPinia()))

describe('permissions store timeout', () => {
  it('reads the running timeout and its reason', () => {
    const permissions = usePermissionsStore()
    expect(permissions.timeoutUntil).toBeNull()
    expect(permissions.timeoutReason).toBe('')
    permissions.info = { owner: false, permissions: 0, role_ids: [], top_position: 0, max_upload_mb: 50, catalog: [], channels: [], timeout_until: '2026-01-01T12:00:00Z', timeout_reason: 'spam' }
    expect(permissions.timeoutUntil).toBe(Date.parse('2026-01-01T12:00:00Z'))
    expect(permissions.timeoutReason).toBe('spam')
  })
})
