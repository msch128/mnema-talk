import type { Message, ReactionSummary, ReplyPreview } from '../../types/domain'
import { decodeMessage } from '../../types/domain'

/** A native typed-event projection, enriched by the same native metadata owner.
 * This closed text schema cannot carry attachment, pin, or server plaintext data.
 * Neither author metadata nor the comparison revision authorizes a mutation.
 */
export interface NativeTextRow {
  id: string; number: number; channel_id: string; client_event_id: string; account_id: string; device_id: string
  body: string; parent_id: string | null; reply_to_id: string | null
  created_at: string; updated_at: string; revision_id: string
  revision_account_id: string; revision_device_id: string
  edited: boolean; deleted: boolean; reactions: Record<string, string[]>; cached_reply_count: number
  author: { id: string; username: string; display_name: string; avatar_url: string }
  attachments: []; is_pinned: false; mentions: []
}
export interface NativeTextSnapshot { channel_id: string; messages: NativeTextRow[] }

const RECEIPT_ID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/
const ZERO_RECEIPT_ID = '00000000-0000-0000-0000-000000000000'
const ROW_FIELDS = ['id', 'number', 'channel_id', 'client_event_id', 'account_id', 'device_id', 'body', 'parent_id', 'reply_to_id', 'created_at', 'updated_at', 'revision_id', 'revision_account_id', 'revision_device_id', 'edited', 'deleted', 'reactions', 'cached_reply_count', 'author', 'attachments', 'is_pinned', 'mentions'] as const
const RECEIPT_FIELDS = ['id', 'channel_id', 'client_event_id', 'account_id', 'device_id', 'revision_id', 'revision_account_id', 'revision_device_id'] as const
const AUTHOR_FIELDS = ['id', 'username', 'display_name', 'avatar_url'] as const
const SNAPSHOT_FIELDS = ['channel_id', 'messages'] as const
const MAX_ROWS = 1024
const MAX_SNAPSHOT_BYTES = 512 * 1024
const MAX_EMOJI = 64
const MAX_EMOJI_BYTES = 128
const MAX_REACTORS = 1024
const MAX_REPLY_COUNT = 1024

function object(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

export function nativeReceiptId(value: unknown): value is string {
  return typeof value === 'string' && RECEIPT_ID.test(value) && value !== ZERO_RECEIPT_ID
}

function exact(value: Record<string, unknown>, names: readonly string[]): boolean {
  return Object.keys(value).length === names.length && names.every(name => Object.hasOwn(value, name))
}

function optionalId(value: unknown): boolean {
  return value === null || nativeReceiptId(value)
}

function fail(): never {
  throw new Error('Invalid native typed chat projection')
}

function rowRecord(value: unknown): Record<string, unknown> {
  if (!object(value)) return fail()
  if (!exact(value, ROW_FIELDS)) return fail()
  return value
}

function requireReceipts(value: Record<string, unknown>): void {
  for (const name of RECEIPT_FIELDS) {
    if (!nativeReceiptId(value[name])) return fail()
  }
}

function requireCounters(value: Record<string, unknown>): void {
  if (!Number.isSafeInteger(value['number'])) return fail()
  if (Number(value['number']) < 1) return fail()
  if (!Number.isSafeInteger(value['cached_reply_count'])) return fail()
  if (Number(value['cached_reply_count']) < 0) return fail()
  if (Number(value['cached_reply_count']) > MAX_REPLY_COUNT) return fail()
}

function requireRowText(value: Record<string, unknown>): void {
  if (!optionalId(value['parent_id'])) return fail()
  if (!optionalId(value['reply_to_id'])) return fail()
  if (typeof value['edited'] !== 'boolean') return fail()
  if (typeof value['deleted'] !== 'boolean') return fail()
  if (typeof value['body'] !== 'string') return fail()
  if (typeof value['created_at'] !== 'string') return fail()
  if (typeof value['updated_at'] !== 'string') return fail()
}

function requireClosedLists(value: Record<string, unknown>): void {
  const attachments = value['attachments']
  if (!Array.isArray(attachments)) return fail()
  if (attachments.length !== 0) return fail()
  if (value['is_pinned'] !== false) return fail()
  const mentions = value['mentions']
  if (!Array.isArray(mentions)) return fail()
  if (mentions.length !== 0) return fail()
}

function requireAuthor(value: Record<string, unknown>): void {
  const author = value['author']
  if (!object(author)) return fail()
  if (!exact(author, AUTHOR_FIELDS)) return fail()
  if (author['id'] !== value['account_id']) return fail()
  if (typeof author['username'] !== 'string') return fail()
  if (!author['username']) return fail()
  if (typeof author['display_name'] !== 'string') return fail()
  if (typeof author['avatar_url'] !== 'string') return fail()
}

function requireReaction(emoji: string, users: unknown): void {
  if (!emoji) return fail()
  if (new TextEncoder().encode(emoji).length > MAX_EMOJI_BYTES) return fail()
  if (!Array.isArray(users)) return fail()
  if (users.length > MAX_REACTORS) return fail()
  if (!users.every(nativeReceiptId)) return fail()
  if (new Set(users).size !== users.length) return fail()
}

function requireReactions(value: Record<string, unknown>): void {
  const reactions = value['reactions']
  if (!object(reactions)) return fail()
  if (Object.keys(reactions).length > MAX_EMOJI) return fail()
  for (const [emoji, users] of Object.entries(reactions)) requireReaction(emoji, users)
}

function row(value: unknown): NativeTextRow {
  const record = rowRecord(value)
  requireReceipts(record)
  requireCounters(record)
  requireRowText(record)
  requireClosedLists(record)
  requireAuthor(record)
  requireReactions(record)
  const typed = record as unknown as NativeTextRow
  // The actual shared decoder validates dates/body bounds and display fields.
  baseMessage(typed, typed.channel_id)
  return structuredClone(typed)
}

function reactionSummaries(reactions: Record<string, string[]>): ReactionSummary[] {
  return Object.entries(reactions).map(([emoji, users]) => ({ emoji, users: [...users], count: users.length }))
}

function baseMessage(value: NativeTextRow, channel: string): Message {
  return decodeMessage({
    id: value.id, number: value.number, channel_id: channel, user_id: value.account_id,
    username: value.author.username, display_name: value.author.display_name,
    ...(value.author.avatar_url !== undefined ? { avatar_url: value.author.avatar_url } : {}),
    content: value.body, created_at: value.created_at, updated_at: value.updated_at, is_edited: value.edited,
    // These are absent by definition of this versioned native text domain.
    attachments: value.attachments, is_pinned: value.is_pinned, mentions: value.mentions,
    reactions: reactionSummaries(value.reactions), reply_count: value.cached_reply_count,
    ...(value.parent_id ? { parent_id: value.parent_id } : {}),
    ...(value.reply_to_id ? { reply_to_id: value.reply_to_id } : {}),
  })
}

function batchMessages(value: unknown, channel: string): unknown[] {
  if (!object(value)) return fail()
  if (!exact(value, SNAPSHOT_FIELDS)) return fail()
  if (value['channel_id'] !== channel) return fail()
  if (!nativeReceiptId(channel)) return fail()
  const messages = value['messages']
  if (!Array.isArray(messages)) return fail()
  if (messages.length > MAX_ROWS) return fail()
  if (new TextEncoder().encode(JSON.stringify(value)).length > MAX_SNAPSHOT_BYTES) return fail()
  return messages
}

function copiedChannel(channels: Map<string, Map<string, NativeTextRow>>, channel: string): Map<string, NativeTextRow> {
  const stored = channels.get(channel)
  if (stored === undefined) return new Map()
  return new Map(stored)
}

function numberTaken(rows: Map<string, NativeTextRow>, decoded: NativeTextRow): boolean {
  for (const old of rows.values()) {
    if (old.id !== decoded.id && old.number === decoded.number) return true
  }
  return false
}

function admitOne(rows: Map<string, NativeTextRow>, delivered: Set<string>, input: unknown, channel: string): void {
  const decoded = row(input)
  if (decoded.channel_id !== channel) return fail()
  if (delivered.has(decoded.id)) return fail()
  if (numberTaken(rows, decoded)) return fail()
  rows.set(decoded.id, decoded)
  delivered.add(decoded.id)
}

function admitRows(rows: Map<string, NativeTextRow>, messages: readonly unknown[], channel: string): void {
  const delivered = new Set<string>()
  for (const input of messages) admitOne(rows, delivered, input, channel)
}

function requireParent(rows: Map<string, NativeTextRow>, value: NativeTextRow): void {
  const parentId = value.parent_id
  if (!parentId) return
  const root = rows.get(parentId)
  if (parentId === value.id) return fail()
  if (!root) return
  if (root.parent_id) return fail()
  if (value.deleted) return
  if (root.deleted) return fail()
}

function requireRowLinks(rows: Map<string, NativeTextRow>, value: NativeTextRow): void {
  requireParent(rows, value)
  if (value.reply_to_id === value.id) return fail()
}

function requireLinks(rows: Map<string, NativeTextRow>): void {
  for (const value of rows.values()) requireRowLinks(rows, value)
}

function byNumber(left: NativeTextRow, right: NativeTextRow): number {
  return left.number - right.number
}

function replyPreview(quoted: NativeTextRow): ReplyPreview {
  const preview: ReplyPreview = {
    id: quoted.id, user_id: quoted.account_id, username: quoted.author.username,
    display_name: quoted.author.display_name, deleted: quoted.deleted, has_attachments: false,
  }
  if (!quoted.deleted) preview.content = quoted.body
  if (quoted.author.avatar_url !== undefined) preview.avatar_url = quoted.author.avatar_url
  return preview
}

function quotedRow(rows: Map<string, NativeTextRow>, value: NativeTextRow): NativeTextRow | null {
  const replyId = value.reply_to_id
  if (!replyId) return null
  const quoted = rows.get(replyId)
  if (!quoted) return null
  return quoted
}

function presentRow(rows: Map<string, NativeTextRow>, value: NativeTextRow, channel: string): Message {
  const message = baseMessage(value, channel)
  const quoted = quotedRow(rows, value)
  if (quoted) message.reply_to = replyPreview(quoted)
  return decodeMessage(message)
}

function visibleMessages(rows: Map<string, NativeTextRow>, channel: string): Message[] {
  const visible: NativeTextRow[] = []
  for (const value of rows.values()) {
    if (!value.deleted) visible.push(value)
  }
  visible.sort(byNumber)
  const messages: Message[] = []
  for (const value of visible) messages.push(presentRow(rows, value, channel))
  return messages
}

export class NativeChatView {
  private channels = new Map<string, Map<string, NativeTextRow>>()

  clear(): void {
    this.channels.clear()
  }

  accept(value: unknown, expectedChannel: string): Message[] {
    const incoming = batchMessages(value, expectedChannel)
    const next = copiedChannel(this.channels, expectedChannel)
    admitRows(next, incoming, expectedChannel)
    if (next.size > MAX_ROWS) return fail()
    // Validate references against this exact admitted snapshot before replacing
    // anything. Missing native provenance never becomes an invented preview.
    requireLinks(next)
    const messages = visibleMessages(next, expectedChannel)
    // Native owns durable history/cursors. Renderer keeps one current admitted
    // channel snapshot; this is neither MLS recovery nor an old-epoch replay.
    this.channels = new Map([[expectedChannel, next]])
    return messages
  }

  lookup(id: string): { channel: string; row: NativeTextRow } | null {
    for (const [channel, rows] of this.channels) {
      const value = rows.get(id)
      if (value) return { channel, row: structuredClone(value) }
    }
    return null
  }

  messages(channel: string): Message[] {
    const rows = this.channels.get(channel)
    if (!rows) return []
    return this.accept({ channel_id: channel, messages: [...rows.values()] }, channel)
  }
}
