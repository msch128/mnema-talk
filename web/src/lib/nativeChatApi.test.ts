import { afterEach, expect, it, vi } from 'vitest'
import { NativeTypedChatApi, type NativeTypedChatBridge } from './nativeChatApi'
import type { StatusChannel } from './trustPort'

const context = '10000000-0000-4000-8000-000000000001'
const channelId = '20000000-0000-4000-8000-000000000001'
const receiptId = '30000000-0000-4000-8000-000000000001'
const accountId = '40000000-0000-4000-8000-000000000001'
const eventId = '50000000-0000-4000-8000-000000000001'
const deviceId = '60000000-0000-4000-8000-000000000001'
const revisionId = '70000000-0000-4000-8000-000000000001'
afterEach(() => vi.useRealTimers())

function row() {
  return { id: receiptId, number: 1, channel_id: channelId, client_event_id: eventId,
    account_id: accountId, device_id: deviceId, body: 'actual fixture text', parent_id: null, reply_to_id: null,
    created_at: '2026-10-08T12:00:00.000Z', updated_at: '2026-10-08T12:00:00.000Z', revision_id: revisionId,
    revision_account_id: accountId, revision_device_id: deviceId, edited: false, deleted: false,
    reactions: {}, cached_reply_count: 0, author: { id: accountId, username: 'fixture-user', display_name: 'Fixture User', avatar_url: '' },
    attachments: [], is_pinned: false, mentions: [] }
}

function fixture() {
  let current = true
  const callback: StatusChannel = { onmessage: () => {} }
  const call = vi.fn<NativeTypedChatBridge['call']>().mockImplementation(async () => {
    callback.onmessage({ channel_id: channelId, messages: [row()] })
    return { context, status: 200, body: { state: 'completed' } }
  })
  const bridge: NativeTypedChatBridge = { context, isCurrent: () => current, call, channel: () => callback }
  return { api: new NativeTypedChatApi(), bridge, call, callback, retire: () => { current = false } }
}

it('feeds genuine native author, receipt and timestamp fields into the shared message decoder', async () => {
  const { api, bridge, call } = fixture()
  expect(await api.handle(`/api/channels/${channelId}/messages?limit=50`, { method: 'GET' }, bridge)).toMatchObject({
    status: 200, body: [{ id: receiptId, number: 1, user_id: accountId, username: 'fixture-user',
      display_name: 'Fixture User', content: 'actual fixture text', created_at: '2026-10-08T12:00:00.000Z' }]
  })
  expect(call).toHaveBeenCalledWith('native_chat_snapshot', expect.objectContaining({ channelId }), undefined)
})

it('maps shared create to a typed operation and requires the exact native event receipt', async () => {
  const { api, bridge, call, callback } = fixture()
  call.mockImplementation(async (command, args) => {
    expect(command).toBe('native_chat_mutate')
    expect(args['input']).toEqual({ kind: 'create', body: 'new fixture text' })
    callback.onmessage({ channel_id: channelId, messages: [{ ...row(), body: 'new fixture text', client_event_id: args['clientEventId'], revision_id: args['clientEventId'] }] })
    return { context, status: 200, body: { state: 'completed', message_id: receiptId } }
  })
  expect(await api.handle(`/api/channels/${channelId}/messages`, { method: 'POST', json: { content: 'new fixture text' } }, bridge)).toMatchObject({ status: 201, body: { id: receiptId, content: 'new fixture text' } })
})

it.each(['edit', 'delete', 'reaction'] as const)('maps %s against the admitted receipt and revision', async kind => {
  const { api, bridge, call, callback } = fixture()
  await api.handle(`/api/channels/${channelId}/messages`, { method: 'GET' }, bridge)
  call.mockImplementation(async (_command, args) => {
    if (kind === 'edit') expect(args['input']).toEqual({ kind, target_receipt_id: receiptId, expected_revision: revisionId, body: 'edited text' })
    if (kind === 'delete') expect(args['input']).toEqual({ kind, target_receipt_id: receiptId, expected_revision: revisionId })
    if (kind === 'reaction') expect(args['input']).toEqual({ kind, target_receipt_id: receiptId, emoji: '👍', action: 'toggle' })
    callback.onmessage({ channel_id: channelId, messages: [{ ...row(),
      ...(kind === 'edit' ? { body: 'edited text', edited: true } : {}),
      ...(kind === 'delete' ? { deleted: true } : {}),
      ...(kind === 'reaction' ? { reactions: { '👍': [accountId] } } : {})
    }] })
    return { context, status: 200, body: { state: 'completed', message_id: receiptId } }
  })
  const path = kind === 'reaction' ? `/api/messages/${receiptId}/reactions` : `/api/channels/${channelId}/messages/${receiptId}`
  const request = kind === 'delete' ? { method: 'DELETE' } : kind === 'edit' ? { method: 'PUT', json: { content: 'edited text' } } : { method: 'POST', json: { emoji: '👍' } }
  expect(await api.handle(path, request, bridge)).toMatchObject({ status: kind === 'delete' ? 204 : 200 })
})

it('rejects stale snapshots and mismatched mutation acknowledgements before publication', async () => {
  const stale = fixture()
  stale.call.mockImplementation(async () => {
    stale.callback.onmessage({ channel_id: channelId, messages: [row()] })
    stale.retire()
    return { context, status: 200, body: { state: 'completed' } }
  })
  await expect(stale.api.handle(`/api/channels/${channelId}/messages`, { method: 'GET' }, stale.bridge)).rejects.toMatchObject({ name: 'AbortError' })
  expect(stale.api.view.messages(channelId)).toEqual([])
  const wrong = fixture()
  wrong.call.mockImplementation(async () => {
    wrong.callback.onmessage({ channel_id: channelId, messages: [row()] })
    return { context, status: 200, body: { state: 'completed', message_id: receiptId } }
  })
  await expect(wrong.api.handle(`/api/channels/${channelId}/messages`, { method: 'POST', json: { content: 'different text' } }, wrong.bridge)).rejects.toThrow('Wrong native chat mutation receipt')
  expect(wrong.api.view.messages(channelId)).toEqual([])
})

it('denies renderer authorship fields and unsupported uploads without any native mutation', async () => {
  const { api, bridge, call } = fixture()
  expect(await api.handle(`/api/channels/${channelId}/messages`, { method: 'POST', json: { content: 'hello', user_id: accountId } }, bridge)).toMatchObject({ status: 503 })
  expect(await api.handle(`/api/channels/${channelId}/messages`, { method: 'POST', form: new FormData() }, bridge)).toMatchObject({ status: 503 })
  expect(call).not.toHaveBeenCalled()
})

it('recovers from native channel allocation failure instead of permanently locking the composer', async () => {
  const h = fixture()
  const allocate = vi.fn(h.bridge.channel).mockImplementationOnce(() => { throw new Error('IPC channel unavailable') })
  const bridge = { ...h.bridge, channel: allocate }
  await expect(h.api.handle(`/api/channels/${channelId}/messages`, { method: 'GET' }, bridge)).rejects.toThrow('IPC channel unavailable')
  expect(await h.api.handle(`/api/channels/${channelId}/messages`, { method: 'GET' }, bridge)).toMatchObject({ status: 200 })
})

it('rejects duplicate callback batches and missing completion data without publishing them', async () => {
  for (const mode of ['duplicate', 'unknown-field', 'wrong-state', 'bad-receipt'] as const) {
    const h = fixture()
    h.call.mockImplementation(async () => {
      h.callback.onmessage({ channel_id: channelId, messages: [row()] })
      if (mode === 'duplicate') h.callback.onmessage({ channel_id: channelId, messages: [row()] })
      return { context, status: 200, body: mode === 'unknown-field' ? { state: 'completed', grant: true } :
        mode === 'bad-receipt' ? { state: 'completed', message_id: 'forged' } : { state: mode === 'wrong-state' ? 'pending' : 'completed' } }
    })
    await expect(h.api.handle(`/api/channels/${channelId}/messages`, mode === 'bad-receipt' ? { method: 'POST', json: { content: 'hello' } } : { method: 'GET' }, h.bridge)).rejects.toThrow()
    expect(h.api.view.messages(channelId)).toEqual([])
  }
})

it('waits for a bounded late native callback and refuses completion without one', async () => {
  vi.useFakeTimers()
  const h = fixture()
  h.call.mockImplementation(async () => {
    setTimeout(() => h.callback.onmessage({ channel_id: channelId, messages: [row()] }), 100)
    return { context, status: 200, body: { state: 'completed' } }
  })
  const pending = h.api.handle(`/api/channels/${channelId}/messages`, { method: 'GET' }, h.bridge)
  await vi.advanceTimersByTimeAsync(100)
  expect(await pending).toMatchObject({ status: 200 })
  h.call.mockResolvedValue({ context, status: 200, body: { state: 'completed' } })
  const missing = h.api.handle(`/api/channels/${channelId}/messages`, { method: 'GET' }, h.bridge)
  const rejected = expect(missing).rejects.toThrow('Native chat completion unavailable')
  await vi.advanceTimersByTimeAsync(5000); await rejected
})

it('keeps uncertain mutation identifiers stable only for an explicit retry', async () => {
  const h = fixture(), events: unknown[] = []
  h.call.mockImplementation(async (_command, args) => {
    events.push(args['clientEventId'])
    if (events.length === 1) throw new Error('Lost acknowledgement')
    h.callback.onmessage({ channel_id: channelId, messages: [{ ...row(), body: 'retry body', client_event_id: args['clientEventId'] }] })
    return { context, status: 200, body: { state: 'completed', message_id: receiptId } }
  })
  const request = { method: 'POST', json: { content: 'retry body' } }
  await expect(h.api.handle(`/api/channels/${channelId}/messages`, request, h.bridge)).rejects.toThrow('Lost acknowledgement')
  expect(h.call).toHaveBeenCalledOnce()
  expect(await h.api.handle(`/api/channels/${channelId}/messages`, request, h.bridge)).toMatchObject({ status: 201 })
  expect(events[1]).toBe(events[0])
  await h.api.handle(`/api/channels/${channelId}/messages`, request, h.bridge)
  expect(events[2]).not.toBe(events[0])
})

it('fences a pending callback on clear and prevents concurrent operations until retirement', async () => {
  const h = fixture(); let resolve!: (value: Awaited<ReturnType<NativeTypedChatBridge['call']>>) => void
  h.call.mockImplementation(() => new Promise(r => { resolve = r }))
  const pending = h.api.handle(`/api/channels/${channelId}/messages`, { method: 'GET' }, h.bridge)
  await expect(h.api.handle(`/api/channels/${channelId}/messages`, { method: 'GET' }, h.bridge)).rejects.toThrow('already pending')
  h.api.clear(); h.callback.onmessage({ channel_id: channelId, messages: [row()] })
  resolve({ context, status: 200, body: { state: 'completed' } })
  await expect(pending).rejects.toMatchObject({ name: 'AbortError' })
  expect(h.api.view.messages(channelId)).toEqual([])
})

it('rejects aborted or already retired scopes before allocating or invoking IPC', async () => {
  for (const retired of [false, true]) {
    const h = fixture(), controller = new AbortController()
    if (retired) h.retire(); else controller.abort()
    await expect(h.api.handle(`/api/channels/${channelId}/messages`, { method: 'GET', signal: controller.signal }, h.bridge)).rejects.toMatchObject({ name: 'AbortError' })
    expect(h.call).not.toHaveBeenCalled()
  }
})

it.each(['unexpected=1', 'limit=20', 'limit=50&limit=50', 'before=forged', `before=${receiptId}&after=${receiptId}`])('denies unsupported pagination %s without IPC', async query => {
  const h = fixture()
  expect(await h.api.handle(`/api/channels/${channelId}/messages?${query}`, { method: 'GET' }, h.bridge)).toMatchObject({ status: 503 })
  expect(h.call).not.toHaveBeenCalled()
})

it('maps admitted pagination and thread roots from actual native rows', async () => {
  const h = fixture(), second = '30000000-0000-4000-8000-000000000002'
  h.call.mockImplementation(async () => {
    h.callback.onmessage({ channel_id: channelId, messages: [row(), { ...row(), id: second, number: 2, parent_id: receiptId }] })
    return { context, status: 200, body: { state: 'completed' } }
  })
  for (const kind of ['before', 'after', 'around']) {
    expect(await h.api.handle(`/api/channels/${channelId}/messages?${kind}=${receiptId}`, { method: 'GET' }, h.bridge)).toMatchObject({ status: 200 })
  }
  expect(await h.api.handle(`/api/messages/${receiptId}/thread`, { method: 'GET' }, h.bridge)).toMatchObject({ status: 200, body: { root: { id: receiptId }, replies: [{ id: second, parent_id: receiptId }] } })
  expect(await h.api.handle(`/api/channels/${channelId}/messages?before=${eventId}`, { method: 'GET' }, h.bridge)).toMatchObject({ status: 404 })
  expect(await h.api.handle('/api/other', { method: 'GET' }, h.bridge)).toBeNull()
})
it('propagates native refusal without publishing a delivered snapshot', async () => {
  const h = fixture()
  h.call.mockImplementation(async () => { h.callback.onmessage({ channel_id: channelId, messages: [row()] }); return { context, status: 403, body: { error: 'Denied' } } })
  expect(await h.api.handle(`/api/channels/${channelId}/messages`, { method: 'GET' }, h.bridge)).toMatchObject({ status: 403 })
  expect(h.api.view.messages(channelId)).toEqual([])
})
it('bounds unresolved mutations and still permits explicitly retrying an existing intent', async () => {
  const h = fixture(); h.call.mockRejectedValue(new Error('Unknown ACK'))
  for (let n = 0; n < 64; n++) await expect(h.api.handle(`/api/channels/${channelId}/messages`, { method: 'POST', json: { content: `uncertain-${n}` } }, h.bridge)).rejects.toThrow('Unknown ACK')
  await expect(h.api.handle(`/api/channels/${channelId}/messages`, { method: 'POST', json: { content: 'new intent' } }, h.bridge)).rejects.toThrow('unresolved operations limit')
  expect(h.call).toHaveBeenCalledTimes(64)
  await expect(h.api.handle(`/api/channels/${channelId}/messages`, { method: 'POST', json: { content: 'uncertain-0' } }, h.bridge)).rejects.toThrow('Unknown ACK')
  expect(h.call).toHaveBeenCalledTimes(65)
})
it.each([
  { method: 'GET', json: {} }, { method: 'PUT', json: { content: 'hello' } },
  { method: 'POST', json: { content: '  ' } }, { method: 'POST', json: { content: 'x'.repeat(4001) } },
  { method: 'POST', json: { content: 'hello', parent_id: 'forged' } }, { method: 'POST', json: { content: 'hello', reply_to_id: 'forged' } }
])('denies unsupported timeline inputs without native dispatch', async request => {
  const h = fixture()
  expect(await h.api.handle(`/api/channels/${channelId}/messages`, request, h.bridge)).toMatchObject({ status: 503 })
  expect(h.call).not.toHaveBeenCalled()
})
it('requires an admitted target in the exact channel and valid mutation method', async () => {
  const h = fixture()
  expect(await h.api.handle(`/api/messages/${receiptId}/thread`, { method: 'GET' }, h.bridge)).toMatchObject({ status: 503 })
  await h.api.handle(`/api/channels/${channelId}/messages`, { method: 'GET' }, h.bridge); h.call.mockClear()
  for (const [path, request] of [
    [`/api/channels/${eventId}/messages/${receiptId}`, { method: 'DELETE' }],
    [`/api/channels/${channelId}/messages/${receiptId}`, { method: 'POST' }],
    [`/api/messages/${receiptId}/reactions`, { method: 'POST', json: { emoji: 1 } }],
    [`/api/messages/${receiptId}/thread`, { method: 'POST' }]
  ] as const) expect(await h.api.handle(path, request, h.bridge)).toMatchObject({ status: 503 })
  expect(h.call).not.toHaveBeenCalled()
})
it('maps a real thread reply using the admitted native parent receipt', async () => {
  const h = fixture(), child = '30000000-0000-4000-8000-000000000002'
  h.call.mockImplementation(async (_command, args) => {
    expect(args['input']).toEqual({ kind: 'reply', body: 'thread reply', parent_receipt_id: receiptId, quoted_receipt_id: receiptId })
    h.callback.onmessage({ channel_id: channelId, messages: [row(), { ...row(), id: child, number: 2, body: 'thread reply', parent_id: receiptId, reply_to_id: receiptId, client_event_id: args['clientEventId'] }] })
    return { context, status: 200, body: { state: 'completed', message_id: child } }
  })
  expect(await h.api.handle(`/api/channels/${channelId}/messages`, { method: 'POST', json: { content: 'thread reply', parent_id: receiptId, reply_to_id: receiptId } }, h.bridge)).toMatchObject({ status: 201, body: { id: child, parent_id: receiptId, reply_to_id: receiptId } })
})
