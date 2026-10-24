import type { ApiOptions } from '../lib/api'

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useServerFeaturesStore } from './serverFeatures'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

const health = (features?: string[]) => ({ status: 'ok', version: '1.1.0', web_client_api: 1, ...(features ? { features } : {}) })

beforeEach(() => {
  setActivePinia(createPinia())
  apiMock.mockReset()
})

describe('server features', () => {
  it('knows the features the server lists and asks only once', async () => {
    apiMock.mockResolvedValue(health(['saved', 'blocks', 'ops.status']))
    const features = useServerFeaturesStore()
    expect(features.has('saved')).toBe(false)
    expect(features.opsStatus).toBe(false)
    await Promise.all([features.load(), features.load()])
    await features.load()
    expect(apiMock).toHaveBeenCalledTimes(1)
    expect(features.has('saved')).toBe(true)
    expect(features.opsStatus).toBe(true)
    expect(features.has('forward')).toBe(false)
    expect(features.loaded).toBe(true)
  })

  it('treats a server without a list (1.0.x) as having none', async () => {
    apiMock.mockResolvedValue(health())
    const features = useServerFeaturesStore()
    await features.load()
    expect(features.loaded).toBe(true)
    expect(features.has('saved')).toBe(false)
  })

  it('shows nothing it could not confirm and asks again after a failure', async () => {
    apiMock.mockRejectedValueOnce(new Error('offline'))
    const features = useServerFeaturesStore()
    await features.load()
    expect(features.loaded).toBe(false)
    apiMock.mockResolvedValueOnce(health(['saved']))
    await features.load()
    expect(features.has('saved')).toBe(true)
  })
})
