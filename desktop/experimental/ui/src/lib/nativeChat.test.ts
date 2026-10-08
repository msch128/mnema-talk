import { expect, it, vi } from 'vitest'
import { createNativeChatPresentation, decodeNativeChatDisplay } from './nativeChat'
import type { StatusChannel } from './trustPort'
const CONTEXT = '00000000-0000-4000-8000-000000000001'
const CHANNEL = '00000000-0000-4000-8000-000000000002'
const EVENT = '00000000-0000-4000-8000-000000000003'
const OTHER = '00000000-0000-4000-8000-000000000004'
const row = { id: EVENT, number: 1, channel_id: CHANNEL, client_event_id: EVENT, account_id: CONTEXT, device_id: OTHER, body: ' 🌲 synthetic message\n' }
const ack = { context: CONTEXT, status: 200, body: { state: 'completed' } }
function harness() {
  let current = true; const channel: StatusChannel = { onmessage() {} }
  const invoke = vi.fn(async (_command: string, _args: Record<string, unknown>): Promise<unknown> => { channel.onmessage([row]); return ack })
  const port = createNativeChatPresentation({ context: CONTEXT, isCurrent: () => current, invoke, channel: () => channel }, CHANNEL, CONTEXT)
  return { port, invoke, channel, retire() { current = false } }
}
it('returns only native verified projection after completed ACK, preserving bytes and fixed publish arguments', async () => {
  const h = harness(); expect(await h.port.publish(row.body, EVENT)).toEqual([row])
  expect(h.invoke).toHaveBeenCalledExactlyOnceWith('native_chat_publish', { context: CONTEXT, channelId: CHANNEL, clientEventId: EVENT, body: row.body, onMessages: h.channel })
})
it('supports actual Channel delivery after completed command ACK without optimistic output', async () => {
  const h = harness(); h.invoke.mockImplementation(async () => ack)
  let returned = false; const pending = h.port.receive().then(v => { returned = true; return v })
  await Promise.resolve(); await Promise.resolve(); expect(returned).toBe(false)
  h.channel.onmessage([]); expect(await pending).toEqual([])
})
it('rejects acknowledged publication if its channel batch or ownership is malformed', async () => {
  for (const value of [[{ ...row, channel_id: OTHER }], [{ ...row, account_id: OTHER }], [{ ...row, client_event_id: OTHER }], [{ ...row, body: 'different' }], [], [{ ...row, key: 'forbidden' }]]) {
    const h = harness(); h.invoke.mockImplementation(async () => { h.channel.onmessage(value); return ack })
    await expect(h.port.publish(row.body, EVENT)).rejects.toThrow()
  }
})
it('blocks replaced profile completions, duplicate callback batches, and concurrent requests', async () => {
  const h = harness(); let finish!: (value: unknown) => void
  h.invoke.mockImplementation(() => new Promise(resolve => { finish = resolve }))
  const pending = h.port.receive(); await expect(h.port.receive()).rejects.toThrow('already active')
  h.channel.onmessage([row]); h.retire(); finish(ack)
  await expect(pending).rejects.toMatchObject({ name: 'AbortError' })
  const duplicate = harness(); duplicate.invoke.mockImplementation(async () => { duplicate.channel.onmessage([row]); duplicate.channel.onmessage([row]); return ack })
  await expect(duplicate.port.receive()).rejects.toThrow()
})
it('bounds missing projection delivery and never retries native failed or ambiguous ACKs', async () => {
  vi.useFakeTimers()
  try {
    const h = harness(); h.invoke.mockImplementation(async () => ack)
    const pending = expect(h.port.receive()).rejects.toThrow(); await vi.advanceTimersByTimeAsync(5000); await pending
    expect(h.invoke).toHaveBeenCalledTimes(1)
  } finally { vi.useRealTimers() }
  for (const value of [{ ...ack, status: 503 }, { ...ack, context: OTHER }, { ...ack, body: { state: 'pending' } }]) {
    const h = harness(); h.invoke.mockImplementation(async () => { h.channel.onmessage([row]); return value })
    await expect(h.port.publish(row.body, EVENT)).rejects.toThrow(); expect(h.invoke).toHaveBeenCalledTimes(1)
  }
})
it('enforces receive batch count, distinct ordered IDs, exact public DTO and Unicode byte bounds', async () => {
  for (const value of [Array.from({ length: 11 }, () => row), [row, row], [{ ...row, number: Number.MAX_SAFE_INTEGER + 1 }], [{ ...row, body: '🌲'.repeat(6145) }]]) {
    const h = harness(); h.invoke.mockImplementation(async () => { h.channel.onmessage(value); return ack }); await expect(h.port.receive()).rejects.toThrow()
  }
  const decoded = decodeNativeChatDisplay(row); row.body = 'mutated source'; expect(decoded.body).not.toEqual(row.body); row.body = decoded.body
  const h = harness(); await expect(h.port.publish('🌲'.repeat(6145), EVENT)).rejects.toThrow(); expect(h.invoke).not.toHaveBeenCalled()
  h.port.dispose(); await expect(h.port.receive()).rejects.toMatchObject({ name: 'AbortError' })
})
