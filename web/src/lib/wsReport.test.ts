import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ApiOptions } from './api'
import { reportWebSocket } from './wsReport'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('./api', () => ({ api: (path: string, options?: ApiOptions) => apiMock(path, options) }))

beforeEach(() => { vi.useFakeTimers(); apiMock.mockReset(); apiMock.mockResolvedValue(null) })
afterEach(() => vi.useRealTimers())

describe('reportWebSocket', () => {
  it('reports an open connection at once', async () => {
    await reportWebSocket(() => true)
    expect(apiMock).toHaveBeenCalledWith('/api/admin/status/websocket', { method: 'POST', json: { connected: true } })
  })

  it('gives a connecting socket a moment, then reports it as open', async () => {
    let open = false
    const done = reportWebSocket(() => open)
    await vi.advanceTimersByTimeAsync(500)
    expect(apiMock).not.toHaveBeenCalled()
    open = true
    await vi.advanceTimersByTimeAsync(250)
    await done
    expect(apiMock).toHaveBeenCalledWith('/api/admin/status/websocket', { method: 'POST', json: { connected: true } })
  })

  it('reports a socket that never opens as not connected, after the wait', async () => {
    const done = reportWebSocket(() => false, 1000)
    await vi.advanceTimersByTimeAsync(1000)
    await done
    expect(apiMock).toHaveBeenCalledWith('/api/admin/status/websocket', { method: 'POST', json: { connected: false } })
  })

  it('never throws when the report cannot be sent', async () => {
    apiMock.mockRejectedValue(new Error('offline'))
    await expect(reportWebSocket(() => true)).resolves.toBeUndefined()
  })
})
