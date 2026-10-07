import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { ADMIN_UPDATE_POLL_MS, useAppVersionStore } from './appVersion'
import type { UpdateStatus } from '../types/domain'

function status(update_available = true): UpdateStatus {
  return { check_enabled: true, check_error: '', checked_at: null, current_version: '1.0.0', latest_version: '1.1.0', published_at: null, release_notes: '', release_url: '', retry_at: null, update_available }
}
function respond(body: unknown, code = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status: code, headers: { 'Content-Type': 'application/json' } }))
}
beforeEach(() => { setActivePinia(createPinia()); vi.useFakeTimers() })
afterEach(() => { vi.clearAllTimers(); vi.useRealTimers(); vi.unstubAllGlobals() })

describe('application version notifications', () => {
  it('shows each new server release once and clears an update after reconnect', () => {
    const store = useAppVersionStore()
    expect(store.showReloadBanner).toBe(false)
    expect(store.showUpdatingBanner).toBe(false)
    store.setServerVersion(store.clientVersion)
    store.setUpdating(' v9.9.9 ')
    expect(store.updatingTo).toBe('9.9.9')
    expect(store.showUpdatingBanner).toBe(true)
    store.setUpdating(store.clientVersion)
    expect(store.showUpdatingBanner).toBe(false)
    store.setServerVersion('9.9.9')
    expect(store.updatingTo).toBe('')
    expect(store.reloadAvailable).toBe(true)
    expect(store.showReloadBanner).toBe(true)
    store.dismiss()
    expect(store.showReloadBanner).toBe(false)
    store.setUpdating('10.0.0')
    expect(store.showUpdatingBanner).toBe(false)
    store.setServerVersion('10.0.0')
    expect(store.showReloadBanner).toBe(true)
  })

  it('keeps the last successful release state when the refresh fails', async () => {
    const store = useAppVersionStore()
    expect(store.adminUpdateAvailable).toBe(false)
    const fetch = vi.fn().mockImplementation(() => respond(status()))
    vi.stubGlobal('fetch', fetch)
    await store.refreshAdminUpdate()
    expect(fetch).toHaveBeenCalledWith('/api/admin/system/update', expect.objectContaining({ credentials: 'same-origin' }))
    expect(store.adminUpdateAvailable).toBe(true)
    fetch.mockImplementation(() => respond({ error: 'restart' }, 503))
    await store.refreshAdminUpdate()
    expect(store.adminUpdate).toEqual(status())
    store.setAdminUpdate(status(false))
    expect(store.adminUpdateAvailable).toBe(false)
    store.setAdminUpdate(null)
    expect(store.adminUpdate).toBeNull()
  })

  it('replaces polling and stops when administrator privileges are lost', async () => {
    const store = useAppVersionStore()
    const fetch = vi.fn().mockImplementation(() => respond(status()))
    vi.stubGlobal('fetch', fetch)
    let admin = true
    store.followAdminUpdates(() => admin)
    await vi.advanceTimersByTimeAsync(0)
    expect(fetch).toHaveBeenCalledTimes(1)
    store.followAdminUpdates(() => admin)
    await vi.advanceTimersByTimeAsync(ADMIN_UPDATE_POLL_MS)
    expect(fetch).toHaveBeenCalledTimes(3)
    admin = false
    await vi.advanceTimersByTimeAsync(ADMIN_UPDATE_POLL_MS)
    expect(fetch).toHaveBeenCalledTimes(3)
    expect(store.adminUpdate).toBeNull()
    await vi.advanceTimersByTimeAsync(ADMIN_UPDATE_POLL_MS)
    expect(fetch).toHaveBeenCalledTimes(3)
    store.followAdminUpdates(() => false)
    expect(vi.getTimerCount()).toBe(0)
  })
})
