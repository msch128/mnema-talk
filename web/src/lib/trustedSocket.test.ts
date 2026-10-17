import { beforeEach, expect, it, vi } from 'vitest'

type Listener = ((event: unknown) => void) | null
const mocks = vi.hoisted(() => ({ invoke: vi.fn(), channels: [] as { onmessage: Listener }[] }))
vi.mock('@tauri-apps/api/core', () => ({
  invoke: mocks.invoke,
  Channel: class { onmessage: Listener = null; constructor() { mocks.channels.push(this) } },
}))
import { TrustedSocket } from './trustedSocket'

const emit = (event: unknown) => mocks.channels.at(-1)?.onmessage?.(event)
const flush = async () => { await Promise.resolve(); await Promise.resolve() }
let opened: (id: number) => void
let failed: (error: unknown) => void

beforeEach(() => {
  mocks.channels.length = 0
  mocks.invoke.mockReset()
  mocks.invoke.mockImplementation((command: string) => {
    if (command === 'desktop_ws_open') return new Promise<number>((resolve, reject) => { opened = resolve; failed = reject })
    return Promise.resolve(null)
  })
})

it('asks the host for the socket and follows its events', async () => {
  const socket = new TrustedSocket()
  expect(mocks.invoke).toHaveBeenCalledWith('desktop_ws_open', { path: '/api/ws', onEvent: mocks.channels[0] })
  const seen: string[] = []
  socket.onopen = () => seen.push('open')
  socket.onmessage = event => seen.push(event.data)
  socket.onclose = () => seen.push('close')
  expect(socket.readyState).toBe(0)
  expect(() => socket.send('early')).toThrow(/connecting/)
  emit({ id: 7, kind: 'open' })
  opened(7); await flush()
  expect(socket.readyState).toBe(1)
  emit({ id: 7, kind: 'open' })
  emit({ id: 7, kind: 'message', data: '{"type":"pong"}' })
  socket.send('{"type":"ping"}')
  expect(mocks.invoke).toHaveBeenCalledWith('desktop_ws_send', { id: 7, data: '{"type":"ping"}' })
  emit({ id: 7, kind: 'close' })
  emit({ id: 7, kind: 'message', data: 'late' })
  expect(socket.readyState).toBe(3)
  expect(seen).toEqual(['open', '{"type":"pong"}', 'close'])
  socket.send('after close')
  expect(mocks.invoke).not.toHaveBeenCalledWith('desktop_ws_send', expect.objectContaining({ data: 'after close' }))
})

it('learns its id from the first event when the open reply is late', async () => {
  const socket = new TrustedSocket('/api/ws')
  emit({ id: 3, kind: 'open' })
  socket.send('x')
  expect(mocks.invoke).toHaveBeenCalledWith('desktop_ws_send', { id: 3, data: 'x' })
  opened(3); await flush()
})

it('closes through the host and finishes on the host close event', async () => {
  const socket = new TrustedSocket()
  const onclose = vi.fn()
  socket.onclose = onclose
  emit({ id: 4, kind: 'open' })
  socket.close(); socket.close()
  expect(socket.readyState).toBe(2)
  expect(mocks.invoke.mock.calls.filter(c => c[0] === 'desktop_ws_close')).toEqual([['desktop_ws_close', { id: 4 }]])
  emit({ id: 4, kind: 'message', data: 'ignored while closing' })
  emit({ id: 4, kind: 'open' })
  opened(4); await flush()
  expect(mocks.invoke.mock.calls.filter(c => c[0] === 'desktop_ws_close')).toHaveLength(1)
  expect(onclose).not.toHaveBeenCalled()
  emit({ id: 4, kind: 'close' })
  expect(onclose).toHaveBeenCalledTimes(1)
  socket.close()
})

it('defers a close requested before the host has answered', async () => {
  const socket = new TrustedSocket()
  socket.close()
  expect(mocks.invoke.mock.calls.some(c => c[0] === 'desktop_ws_close')).toBe(false)
  opened(9); await flush()
  expect(mocks.invoke).toHaveBeenCalledWith('desktop_ws_close', { id: 9 })
})

it('reports a refused open as a closed socket', async () => {
  const socket = new TrustedSocket()
  const onclose = vi.fn()
  socket.onclose = onclose
  failed('The host refused'); await flush()
  expect(socket.readyState).toBe(3)
  expect(onclose).toHaveBeenCalledTimes(1)
  failed('again'); await flush()
  expect(onclose).toHaveBeenCalledTimes(1)
})

it('closes when a send cannot be delivered', async () => {
  const socket = new TrustedSocket()
  const onclose = vi.fn()
  socket.onclose = onclose
  emit({ id: 5, kind: 'open' })
  mocks.invoke.mockImplementation(() => Promise.reject(new Error('gone')))
  socket.send('x'); await flush()
  expect(onclose).toHaveBeenCalledTimes(1)
  opened(5); await flush()
})

it('works without handlers and closes cleanly after the host finished first', async () => {
  const socket = new TrustedSocket()
  emit({ id: 6, kind: 'open' })
  emit({ id: 6, kind: 'message', data: 'x' })
  emit({ id: 6, kind: 'close' })
  opened(6); await flush()
  expect(socket.readyState).toBe(3)
  const second = new TrustedSocket()
  emit({ id: 8, kind: 'close' })
  expect(second.readyState).toBe(3)
})

it('does not fail when the host has already dropped the socket on close', async () => {
  const socket = new TrustedSocket()
  emit({ id: 2, kind: 'open' })
  mocks.invoke.mockImplementation(() => Promise.reject(new Error('unknown socket')))
  expect(() => socket.close()).not.toThrow()
  await flush()
  expect(socket.readyState).toBe(2)
})

it('finishes only once when a failed send races the close event', async () => {
  const socket = new TrustedSocket()
  const onclose = vi.fn()
  socket.onclose = onclose
  emit({ id: 1, kind: 'open' })
  mocks.invoke.mockImplementation(() => Promise.reject(new Error('gone')))
  socket.send('x')
  emit({ id: 1, kind: 'close' })
  await flush()
  expect(onclose).toHaveBeenCalledTimes(1)
})
