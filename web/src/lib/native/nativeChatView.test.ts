import { expect, it } from 'vitest'
import { NativeChatView, nativeReceiptId, type NativeTextRow } from './nativeChatView'

const id = (n: number) => `10000000-0000-4000-8000-${String(n).padStart(12, '0')}`
const channel = id(1)
function row(n = 2): NativeTextRow {
  return { id: id(n), number: n, channel_id: channel, client_event_id: id(3), account_id: id(4), device_id: id(5),
    revision_id: id(6), revision_account_id: id(4), revision_device_id: id(5), body: 'Synthetic native message',
    parent_id: null, reply_to_id: null, created_at: '2026-10-08T12:00:00Z', updated_at: '2026-10-08T12:00:00Z',
    edited: false, deleted: false, reactions: {}, cached_reply_count: 0,
    author: { id: id(4), username: 'fixture', display_name: 'Fixture', avatar_url: '' }, attachments: [], is_pinned: false, mentions: [] }
}
const snapshot = (messages: unknown[]) => ({ channel_id: channel, messages })

it('admits authoritative chronology, thread linkage and quoted content without sharing mutable state', () => {
  const view = new NativeChatView()
  const root = row(), reply = { ...row(7), reply_to_id: root.id }, thread = { ...row(8), parent_id: root.id }
  const messages = view.accept(snapshot([thread, reply, root]), channel)
  expect(messages.map(m => m.id)).toEqual([root.id, reply.id, thread.id])
  expect(messages[1]?.reply_to).toMatchObject({ id: root.id, content: root.body, deleted: false })
  expect(messages[2]?.parent_id).toBe(root.id)
  root.body = 'Changed outside view'
  const lookup = view.lookup(root.id)!
  lookup.row.body = 'Changed returned copy'
  expect(view.messages(channel)[0]?.content).toBe('Synthetic native message')
  expect(view.lookup(id(9))).toBeNull()
  expect(view.messages(id(9))).toEqual([])
  view.clear()
  expect(view.lookup(root.id)).toBeNull()
})

it('keeps deleted quote provenance while hiding deleted rows and retiring another channel', () => {
  const view = new NativeChatView(), root = { ...row(), deleted: true }, reply = { ...row(7), reply_to_id: root.id }
  expect(view.accept(snapshot([root, reply]), channel)[0]?.reply_to).toMatchObject({ deleted: true })
  expect(view.messages(channel)[0]?.reply_to).not.toHaveProperty('content')
  view.accept({ channel_id: id(9), messages: [] }, id(9))
  expect(view.lookup(root.id)).toBeNull()
})

it.each([
  ['unknown field', { grant: 'forged' }], ['nil receipt', { id: '00000000-0000-0000-0000-000000000000' }],
  ['unsafe number', { number: Number.MAX_SAFE_INTEGER + 1 }], ['zero number', { number: 0 }],
  ['negative replies', { cached_reply_count: -1 }], ['excess replies', { cached_reply_count: 1025 }],
  ['bad parent', { parent_id: 'forged' }], ['bad quote', { reply_to_id: 42 }], ['bad edit flag', { edited: 1 }],
  ['bad delete flag', { deleted: 'false' }], ['bad body', { body: null }], ['overlong body', { body: 'x'.repeat(4001) }],
  ['invalid creation date', { created_at: 'yesterday' }], ['invalid update date', { updated_at: 1 }],
  ['unsupported attachment', { attachments: [{}] }], ['unsupported pin', { is_pinned: true }],
  ['unsupported mention', { mentions: [id(4)] }], ['unknown author', { author: null }],
  ['forged author', { author: { ...row().author, id: id(9) } }], ['empty username', { author: { ...row().author, username: '' } }],
  ['bad display name', { author: { ...row().author, display_name: null } }], ['bad avatar', { author: { ...row().author, avatar_url: null } }],
  ['bad reactions', { reactions: [] }], ['too many emoji', { reactions: Object.fromEntries(Array.from({ length: 65 }, (_, n) => [String(n), []])) }],
  ['empty emoji', { reactions: { '': [] } }], ['overlong emoji', { reactions: { ['x'.repeat(129)]: [] } }],
  ['bad reactor list', { reactions: { '👍': {} } }], ['invalid reactor', { reactions: { '👍': ['forged'] } }],
  ['duplicate reactor', { reactions: { '👍': [id(4), id(4)] } }], ['too many reactors', { reactions: { '👍': Array(1025).fill(id(4)) } }],
  ['self thread', { parent_id: id(2) }], ['self quote', { reply_to_id: id(2) }], ['wrong channel', { channel_id: id(9) }]
])('rejects %s atomically without replacing admitted content', (_name, changes) => {
  const view = new NativeChatView(); view.accept(snapshot([row()]), channel)
  expect(() => view.accept(snapshot([{ ...row(), ...changes }]), channel)).toThrow()
  expect(view.messages(channel)[0]?.content).toBe('Synthetic native message')
})

it('rejects duplicate receipts, duplicate sequence numbers, nested and live replies to deleted roots', () => {
  for (const rows of [[row(), row()], [row(), { ...row(7), number: 2 }],
    [row(), { ...row(7), parent_id: id(2) }, { ...row(8), parent_id: id(7) }],
    [{ ...row(), deleted: true }, { ...row(7), parent_id: id(2) }]]) {
    expect(() => new NativeChatView().accept(snapshot(rows), channel)).toThrow()
  }
})

it.each([null, [], {}, { ...snapshot([]), extra: true }, { ...snapshot([]), channel_id: id(9) },
  snapshot(Array(1025).fill(row())), snapshot([{ ...row(), body: 'x'.repeat(512 * 1024) }])])('rejects malformed or unbounded batches', value => {
  expect(() => new NativeChatView().accept(value, channel)).toThrow()
})

it('requires canonical nonzero native receipt identifiers', () => {
  expect(nativeReceiptId(id(2))).toBe(true)
  for (const value of [null, 1, id(2).toUpperCase().replace('10000000', 'ABCDEFAB'), '', '00000000-0000-0000-0000-000000000000']) expect(nativeReceiptId(value)).toBe(false)
})
