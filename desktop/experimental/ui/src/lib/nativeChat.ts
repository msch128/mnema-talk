import type { StatusChannel } from './trustPort'
import type { NativeChatCache } from './nativeChatCache'
import { trustOperationId } from './nativeTrust'
export interface NativeChatDisplay {
  id: string; number: number; channel_id: string; client_event_id: string
  account_id: string; device_id: string; body: string
}
export interface NativeChatBridge {
  readonly context: string
  isCurrent(): boolean
  invoke(command: string, args: Record<string, unknown>): Promise<unknown>
  channel(): StatusChannel
}
const nil = '00000000-0000-0000-0000-000000000000'
const uuid = (v: unknown): v is string => trustOperationId(v) && v !== nil
const object = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v)
const exact = (v: Record<string, unknown>, names: readonly string[]) => Object.keys(v).length === names.length && names.every(n => Object.hasOwn(v, n))
export function validNativeChatBody(v: unknown): v is string {
  if (typeof v !== 'string' || v.length === 0 || v.length > 24 * 1024) return false
  for (let i = 0; i < v.length; i++) {
    const unit = v.charCodeAt(i)
    if (unit >= 0xd800 && unit <= 0xdbff) {
      const next = v.charCodeAt(++i)
      if (!(next >= 0xdc00 && next <= 0xdfff)) return false
    } else if (unit >= 0xdc00 && unit <= 0xdfff) return false
  }
  return new TextEncoder().encode(v).length <= 24 * 1024
}
export function decodeNativeChatDisplay(value: unknown): NativeChatDisplay {
  if (!object(value) || !exact(value, ['id', 'number', 'channel_id', 'client_event_id', 'account_id', 'device_id', 'body']) || !uuid(value['id']) || !uuid(value['channel_id']) || !uuid(value['client_event_id']) || !uuid(value['account_id']) || !uuid(value['device_id']) || typeof value['number'] !== 'number' || !Number.isSafeInteger(value['number']) || value['number'] < 1 || !validNativeChatBody(value['body'])) throw new Error('Invalid native chat display')
  return { id: value['id'], number: value['number'], channel_id: value['channel_id'], client_event_id: value['client_event_id'], account_id: value['account_id'], device_id: value['device_id'], body: value['body'] }
}
export function createNativeChatPresentation(bridge: NativeChatBridge, channelId: string, accountId: string, cache?: NativeChatCache) {
  if (!uuid(bridge.context) || !uuid(channelId) || !uuid(accountId)) throw new Error('Invalid native chat presentation')
  let active = false
  let disposed = false
  let wake: (() => void) | null = null
  const current = () => { if (disposed || !bridge.isCurrent()) throw new DOMException('Native chat presentation retired', 'AbortError') }
  async function request(command: 'native_chat_publish' | 'native_chat_receive', payload: Record<string, unknown>, sent?: { event: string; body: string }) {
    current()
    if (active) throw new Error('Native chat request already active')
    active = true
    const received: { rows: NativeChatDisplay[] | null } = { rows: null }
    let invalid = false
    let arrived!: () => void
    const arrival = new Promise<void>(resolve => { arrived = resolve; wake = resolve })
    let timer: ReturnType<typeof setTimeout> | undefined
    const channel = bridge.channel()
    channel.onmessage = value => {
      if (disposed || !bridge.isCurrent()) return
      try {
        if (received.rows !== null || !Array.isArray(value) || value.length > (sent ? 1 : 10) || (sent && value.length !== 1)) throw new Error('Invalid native chat batch')
        const decoded = value.map(decodeNativeChatDisplay)
        const ids = new Set<string>(); let last = 0
        for (const row of decoded) {
          if (row.channel_id !== channelId || ids.has(row.id) || row.number <= last || (sent && (row.account_id !== accountId || row.client_event_id !== sent.event || row.body !== sent.body))) throw new Error('Wrong native chat batch')
          ids.add(row.id); last = row.number
        }
        received.rows = decoded
      } catch { invalid = true }
      arrived()
    }
    try {
      const value = await bridge.invoke(command, { context: bridge.context, channelId, ...payload, onMessages: channel })
      current()
      if (received.rows === null && !invalid && object(value) && value['status'] === 200) {
        await Promise.race([arrival, new Promise<void>(resolve => { timer = setTimeout(resolve, 5000) })])
        current()
      }
      if (!object(value) || !exact(value, ['context', 'status', 'body']) || value['context'] !== bridge.context || value['status'] !== 200 || !object(value['body']) || !exact(value['body'], ['state']) || value['body']['state'] !== 'completed' || invalid || received.rows === null) throw new Error('Native chat request unavailable')
      cache?.store(received.rows)
      return received.rows.map(row => ({ ...row }))
    } finally { if (timer !== undefined) clearTimeout(timer); active = false; wake = null; channel.onmessage = () => {}; received.rows = null }
  }
  return {
    publish(body: string, event: string) {
      if (!validNativeChatBody(body) || !uuid(event)) return Promise.reject(new Error('Invalid native chat input'))
      return request('native_chat_publish', { clientEventId: event, body }, { event, body })
    },
    snapshot() { current(); return cache?.snapshot() ?? [] },
    receive() { return request('native_chat_receive', {}) },
    dispose() { disposed = true; wake?.() }
  }
}
