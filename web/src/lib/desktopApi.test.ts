import { afterEach, expect, it, vi } from 'vitest'

const native = vi.hoisted(() => vi.fn())
vi.mock('./nativeTransport', () => ({ nativeApiTransport: native }))
import { api } from './api'

afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

it('uses same-origin cookies in a browser and never invokes native IPC', async () => {
  const fetch = vi.fn().mockResolvedValue({ status: 200, json: async () => ({ source: 'browser' }) })
  vi.stubGlobal('fetch', fetch)
  expect(await api('/api/auth/me')).toEqual({ source: 'browser' })
  expect(fetch).toHaveBeenCalledWith('/api/auth/me', expect.objectContaining({ credentials: 'same-origin' }))
  expect(native).not.toHaveBeenCalled()
})

it('routes the shared API through the native broker in Tauri without renderer HTTP', async () => {
  vi.stubGlobal('isTauri', true)
  const fetch = vi.fn()
  vi.stubGlobal('fetch', fetch)
  native.mockResolvedValue({ status: 200, body: { source: 'native' } })
  expect(await api('/api/auth/me')).toEqual({ source: 'native' })
  expect(native).toHaveBeenCalledWith('/api/auth/me', { method: 'GET' })
  expect(fetch).not.toHaveBeenCalled()
})

it('fails a rejected native operation without falling back to browser HTTP', async () => {
  vi.stubGlobal('isTauri', true)
  const fetch = vi.fn()
  vi.stubGlobal('fetch', fetch)
  native.mockRejectedValue(new Error('synthetic native failure'))
  await expect(api('/api/auth/me')).rejects.toMatchObject({ status: 0, code: 'NETWORK' })
  expect(fetch).not.toHaveBeenCalled()
})
